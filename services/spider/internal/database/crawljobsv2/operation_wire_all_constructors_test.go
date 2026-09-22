package crawljobsv2

import (
	"bytes"
	"crypto/sha1" // #nosec G505 -- Redis EVALSHA identity is normatively SHA-1.
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
)

const wireOracleConstructorCount = 43

type wireOracleGateVariant uint8

const (
	wireOracleUngated wireOracleGateVariant = iota + 1
	wireOracleBootOnly
	wireOracleActive
	wireOracleCandidateBeforeRetirement
	wireOracleCandidateAfterRetirement
)

type wireOracleTailKind uint8

const (
	wireOracleNoTail wireOracleTailKind = iota + 1
	wireOracleRecordTail
	wireOracleActiveRunIDTail
)

type wireOracleKeyPlan uint8

const (
	wireOracleKeysApproveBoot wireOracleKeyPlan = iota + 1
	wireOracleKeysAuthority
	wireOracleKeysRetire
	wireOracleKeysPromote
	wireOracleKeysMarkShutdown
	wireOracleKeysCreateRun
	wireOracleKeysRunRecords
	wireOracleKeysRun
	wireOracleKeysJob
	wireOracleKeysReservation
	wireOracleKeysStage
	wireOracleKeysRunMaintenance
	wireOracleKeysArchive
	wireOracleKeysPurge
	wireOracleKeysCleanStage
	wireOracleKeysRateMaintenance
)

type wireOracleOperationExpectation struct {
	operation      OperationName
	scriptSource   string
	semanticFields []string
	recordFields   []string
	tailKind       wireOracleTailKind
	tailCount      int
	countField     string
	keyPlan        wireOracleKeyPlan
	requestLimit   uint64
	variants       []wireOracleGateVariant
}

type wireOracleScriptIdentity struct {
	source       []byte
	redisSHA1    string
	sourceSHA256 Digest
}

// TestOperationWireAllConstructorsReadOnlyOracle deliberately treats every
// constructed request as read-only. Its field and key expectations are literal
// test-owned contract data; it does not consult operationWireSpecifications,
// operationWireOrder, expectedOperationWireKeys, or their helper plans.
func TestOperationWireAllConstructorsReadOnlyOracle(t *testing.T) {
	expectations := wireOracleOperationExpectations()
	wireOracleAssertInventory(t, expectations)
	fixture := newCanonicalWireOracleFixture(t)
	wireOracleAssertSameShapedCanaries(t, fixture, expectations)
	bindings, scriptIdentities := canonicalWireOracleBindings(t, expectations)

	builtCount := 0
	for _, expectation := range expectations {
		expectation := expectation
		for _, variant := range expectation.variants {
			variant := variant
			t.Run(string(expectation.operation)+"/"+wireOracleVariantName(variant), func(t *testing.T) {
				gate := wireOracleGate(t, fixture, expectation.operation, variant)
				request, err := wireOracleConstructRequest(fixture, expectation.operation, variant, gate)
				if err != nil {
					t.Fatalf("construct %s: %v", expectation.operation, err)
				}

				// Every successfully constructed variant must traverse the public,
				// complete-binding production build boundary.
				built, err := BuildEvalSHARequest(bindings, request)
				if err != nil {
					t.Fatalf("BuildEvalSHARequest(%s): %v", expectation.operation, err)
				}
				builtCount++

				wireOracleAssertOperation(t, expectation, request, built)
				prefixCount := wireOracleAssertGate(t, fixture, expectation.operation, variant, request, built)
				wireOracleAssertScript(t, expectation, scriptIdentities[expectation.operation], built)
				wireOracleAssertSemanticAndTail(t, fixture, expectation, variant, prefixCount, request, built)
				wireOracleAssertKeys(t, fixture, expectation, request, built)
				wireOracleAssertSize(t, expectation, built)
			})
		}
	}

	if builtCount != 52 {
		t.Fatalf("built constructor variants = %d, want 52", builtCount)
	}
}

func TestOperationWireOracleRejectsBundleContractSubstitution(t *testing.T) {
	fixture := newWireOracleFixture(t)
	bundle, _ := newWireOracleScriptBindingSet(t, wireOracleOperationExpectations(), fixture.artifacts.contract)
	otherContract := Digest(strings.Repeat("b", 64))
	bundle.approvedContractSHA256 = otherContract
	bundle.seal.approvedContractSHA256 = otherContract
	bundle.seal.bundleSHA256 = deriveScriptBindingSetSeal(bundle.sourceSetSHA256, otherContract)
	if err := bundle.validate(); err != nil {
		t.Fatalf("independently sealed test bundle is invalid: %v", err)
	}

	gate := wireOracleGate(t, fixture, OperationMaintainRateScopes, wireOracleActive)
	request, err := wireOracleConstructRequest(fixture, OperationMaintainRateScopes, wireOracleActive, gate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildEvalSHARequest(bundle, request); !errors.Is(err, ErrScriptContractMismatch) {
		t.Fatalf("request/bundle contract mismatch error = %v", err)
	}
}

func TestOperationWireOracleRejectsTargetedSemanticFieldSubstitutions(t *testing.T) {
	fixture := newWireOracleFixture(t)
	bundle, _ := newWireOracleScriptBindingSet(t, wireOracleOperationExpectations(), fixture.artifacts.contract)
	tests := []struct {
		name  string
		left  string
		right string
		want  error
	}{
		{name: "run ID replaced by owner ID", left: "run_id", right: "owner_id", want: ErrOperationWireKeys},
		{name: "job ID replaced by lease token", left: "job_id", right: "lease_token", want: ErrOperationWireKeys},
		{name: "commit digest replaced by chunk digest", left: "commit_id", right: "chunk_digest", want: ErrOperationWireChunk},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gate := wireOracleGate(t, fixture, OperationStageOutlinksBatch, wireOracleActive)
			request, err := wireOracleConstructRequest(fixture, OperationStageOutlinksBatch, wireOracleActive, gate)
			if err != nil {
				t.Fatal(err)
			}
			values := operationWireSemanticValues(request.semantic)
			for index := range request.semantic {
				if request.semantic[index].Name == test.left {
					request.semantic[index].Value = []byte(values[test.right])
				}
			}
			if _, err := BuildEvalSHARequest(bundle, request); !errors.Is(err, test.want) {
				t.Fatalf("field substitution error = %v, want %v", err, test.want)
			}
		})
	}
}

func wireOracleAssertInventory(t *testing.T, expectations []wireOracleOperationExpectation) {
	t.Helper()
	if len(expectations) != wireOracleConstructorCount {
		t.Fatalf("literal constructor expectations = %d, want %d", len(expectations), wireOracleConstructorCount)
	}
	if len(operations) != wireOracleConstructorCount {
		t.Fatalf("production operation inventory = %d, oracle covers %d; update the constructor oracle", len(operations), wireOracleConstructorCount)
	}

	seen := make(map[OperationName]struct{}, len(expectations))
	sources := make(map[string]struct{}, len(expectations))
	var candidateBefore, candidateAfter, activeAndCandidate []OperationName
	variantCount := 0
	for _, expectation := range expectations {
		if _, duplicate := seen[expectation.operation]; duplicate {
			t.Fatalf("duplicate literal expectation for %s", expectation.operation)
		}
		seen[expectation.operation] = struct{}{}
		if _, exists := operations[expectation.operation]; !exists {
			t.Fatalf("literal expectation names unknown operation %s", expectation.operation)
		}
		if expectation.scriptSource == "" || expectation.tailKind == 0 || expectation.keyPlan == 0 || expectation.requestLimit == 0 || len(expectation.variants) == 0 {
			t.Fatalf("incomplete literal expectation for %s", expectation.operation)
		}
		if _, duplicate := sources[expectation.scriptSource]; duplicate {
			t.Fatalf("duplicate script source name %q", expectation.scriptSource)
		}
		sources[expectation.scriptSource] = struct{}{}
		fieldNames := make(map[string]struct{}, len(expectation.semanticFields))
		for _, name := range expectation.semanticFields {
			if name == "" {
				t.Fatalf("empty semantic field in literal expectation for %s", expectation.operation)
			}
			if _, duplicate := fieldNames[name]; duplicate {
				t.Fatalf("duplicate semantic field %q for %s", name, expectation.operation)
			}
			fieldNames[name] = struct{}{}
		}
		if expectation.tailKind == wireOracleNoTail {
			if expectation.tailCount != 0 || expectation.countField != "" || len(expectation.recordFields) != 0 {
				t.Fatalf("unexpected tail metadata for %s", expectation.operation)
			}
		} else if _, exists := fieldNames[expectation.countField]; !exists || expectation.tailCount <= 0 {
			t.Fatalf("incomplete count linkage for %s", expectation.operation)
		}

		hasActive := false
		hasCandidateBefore := false
		for _, variant := range expectation.variants {
			variantCount++
			switch variant {
			case wireOracleActive:
				hasActive = true
			case wireOracleCandidateBeforeRetirement:
				hasCandidateBefore = true
				candidateBefore = append(candidateBefore, expectation.operation)
			case wireOracleCandidateAfterRetirement:
				candidateAfter = append(candidateAfter, expectation.operation)
			case wireOracleUngated, wireOracleBootOnly:
			default:
				t.Fatalf("unknown gate variant %d for %s", variant, expectation.operation)
			}
		}
		if hasActive && hasCandidateBefore {
			activeAndCandidate = append(activeAndCandidate, expectation.operation)
		}
	}
	for operation := range operations {
		if _, exists := seen[operation]; !exists {
			t.Fatalf("production operation %s has no literal constructor expectation", operation)
		}
	}
	if variantCount != 52 {
		t.Fatalf("literal constructor variants = %d, want 52", variantCount)
	}

	wireOracleAssertOperationNames(t, "candidate pre-retirement variants", candidateBefore, []OperationName{
		OperationRetireLegacyKeys,
		OperationCreateRun,
		OperationEnqueueBatch,
		OperationBeginRunAudit,
		OperationAuditRunBatch,
		OperationSealRun,
		OperationCancelRun,
		OperationCancelBatch,
		OperationPurgeRunBatch,
	})
	wireOracleAssertOperationNames(t, "candidate post-retirement variants", candidateAfter, []OperationName{
		OperationRetireLegacyKeys,
		OperationPromoteCandidateContracts,
	})
	wireOracleAssertOperationNames(t, "active/candidate dual variants", activeAndCandidate, []OperationName{
		OperationCreateRun,
		OperationEnqueueBatch,
		OperationBeginRunAudit,
		OperationAuditRunBatch,
		OperationSealRun,
		OperationCancelRun,
		OperationCancelBatch,
		OperationPurgeRunBatch,
	})
}

func wireOracleAssertSameShapedCanaries(
	t *testing.T,
	fixture *wireOracleFixture,
	expectations []wireOracleOperationExpectation,
) {
	t.Helper()
	for _, expectation := range expectations {
		for _, field := range expectation.semanticFields {
			if !wireOracleRequiresDigestCanary(field) {
				continue
			}
			value, targeted := wireOracleTargetedSemanticValue(
				fixture, expectation.operation, expectation.variants[0], field,
			)
			if !targeted {
				t.Fatalf("%s same-shaped field %q has no independent canary", expectation.operation, field)
			}
			if value == "" && !(expectation.operation == OperationStagePageFields && field == "render_policy_sha256") {
				t.Fatalf("%s same-shaped field %q has an empty canary", expectation.operation, field)
			}
		}
	}

	compatibility := fixture.artifacts.marker.artifact.input
	core := fixture.artifacts.core.input
	independent := map[string]string{
		"boot evidence":                  string(fixture.bootEvidenceSHA256),
		"planned shutdown evidence":      string(fixture.plannedShutdownEvidenceSHA256),
		"install process-stop evidence":  string(fixture.installProcessStopEvidenceSHA256),
		"shutdown process-stop evidence": string(fixture.shutdownProcessStopEvidenceSHA256),
		"source":                         string(fixture.sourceSHA256),
		"authorization":                  string(fixture.authorizationSHA256),
		"authorization scope":            string(fixture.authorizationScopeSHA256),
		"canonicalization":               string(fixture.canonicalizationSHA256),
		"crawl policy":                   string(fixture.crawlPolicySHA256),
		"render policy":                  string(fixture.renderPolicySHA256),
		"archive":                        string(fixture.archiveSHA256),
		"purge evidence":                 string(fixture.purgeEvidenceSHA256),
		"output":                         string(fixture.outputDigest),
		"contract":                       string(fixture.artifacts.contract),
		"redis config":                   string(core.RedisConfigSHA256),
		"maximum shape":                  string(core.MaximumShapeSHA256),
		"memory fixture":                 string(core.MemoryFixtureSHA256),
		"Lua benchmark":                  string(core.LuaBenchmarkSHA256),
		"AOF crash evidence":             string(core.AOFCrashEvidenceSHA256),
		"freeze process-stop evidence":   string(fixture.artifacts.freeze.input.ProcessStopEvidenceSHA256),
		"legacy backup":                  string(fixture.retireInput.BackupSHA256),
		"legacy v1 source":               string(fixture.retireInput.V1SourceSHA256),
		"legacy v1 queue evidence":       string(fixture.retireInput.V1QueueEvidenceSHA256),
		"legacy v1 URLs evidence":        string(fixture.retireInput.V1URLsEvidenceSHA256),
		"legacy v1 depths evidence":      string(fixture.retireInput.V1DepthsEvidenceSHA256),
		"legacy spider queue evidence":   string(fixture.retireInput.SpiderQueueEvidenceSHA256),
		"legacy signal queue evidence":   string(fixture.retireInput.SignalQueueEvidenceSHA256),
		"spider image":                   string(compatibility.SpiderImage),
		"seed importer image":            string(compatibility.SeedImporterImage),
		"crawl admin image":              string(compatibility.CrawlAdminImage),
		"indexer image":                  string(compatibility.IndexerImage),
		"image indexer image":            string(compatibility.ImageIndexerImage),
		"backlinks processor image":      string(compatibility.BacklinksProcessorImage),
		"monitoring image":               string(compatibility.MonitoringImage),
	}
	seen := make(map[string]string, len(independent))
	for name, value := range independent {
		if prior, duplicate := seen[value]; duplicate {
			t.Fatalf("same-shaped canaries %q and %q unexpectedly share %q", prior, name, value)
		}
		seen[value] = name
	}
}

func wireOracleRequiresDigestCanary(field string) bool {
	return strings.Contains(field, "sha256") || strings.HasSuffix(field, "_digest") ||
		strings.HasSuffix(field, "_scope_id") || strings.HasSuffix(field, "_image") ||
		field == "reservation_id" || field == "commit_id" ||
		field == "expected_commit_id" || field == "publication_id" || field == "transition_id"
}

func wireOracleAssertOperation(
	t *testing.T,
	expectation wireOracleOperationExpectation,
	request OperationWireRequest,
	built EvalSHARequest,
) {
	t.Helper()
	if request.Operation() != expectation.operation || built.Operation() != expectation.operation {
		t.Fatalf("operation = request:%s built:%s, want %s", request.Operation(), built.Operation(), expectation.operation)
	}
}

func wireOracleAssertGate(
	t *testing.T,
	fixture *wireOracleFixture,
	operation OperationName,
	variant wireOracleGateVariant,
	request OperationWireRequest,
	built EvalSHARequest,
) int {
	t.Helper()
	arguments := built.Arguments()
	if variant == wireOracleUngated {
		if request.hasGate || operation != OperationApproveBoot {
			t.Fatalf("%s gate presence = %t, want no gate", operation, request.hasGate)
		}
		return 0
	}
	if !request.hasGate {
		t.Fatalf("%s omitted its required gate prefix", operation)
	}
	if len(arguments) < 7 {
		t.Fatalf("%s arguments = %d, want seven-field gate prefix", operation, len(arguments))
	}

	wantMode, wantPhase, wantPresence := wireOracleGateShape(variant)
	if request.gate.Operation() != operation || request.gate.Mode() != wantMode || request.gate.CandidatePhase() != wantPhase {
		t.Fatalf("%s gate = operation:%s mode:%s phase:%s, want operation:%s mode:%s phase:%s",
			operation, request.gate.Operation(), request.gate.Mode(), request.gate.CandidatePhase(), operation, wantMode, wantPhase)
	}
	gateArguments, err := request.gate.Arguments()
	if err != nil {
		t.Fatalf("%s gate arguments: %v", operation, err)
	}
	if len(gateArguments) != 7 {
		t.Fatalf("%s gate argument count = %d, want 7", operation, len(gateArguments))
	}
	for index := 0; index < 7; index++ {
		if !bytes.Equal(arguments[index], gateArguments[index]) {
			t.Fatalf("%s gate argument %d was not bound verbatim", operation, index)
		}
		if (len(arguments[index]) != 0) != wantPresence[index] {
			t.Fatalf("%s gate argument %d presence = %t, want %t", operation, index, len(arguments[index]) != 0, wantPresence[index])
		}
	}
	if string(arguments[0]) != string(wantMode) || string(arguments[1]) != fixture.artifacts.bootEpoch {
		t.Fatalf("%s gate mode/boot epoch = %q/%q, want %q/%q", operation, arguments[0], arguments[1], wantMode, fixture.artifacts.bootEpoch)
	}
	return 7
}

func wireOracleAssertScript(
	t *testing.T,
	expectation wireOracleOperationExpectation,
	identity wireOracleScriptIdentity,
	built EvalSHARequest,
) {
	t.Helper()
	if len(identity.source) == 0 {
		t.Fatalf("%s has no independent script source identity", expectation.operation)
	}
	if built.ScriptName() != expectation.scriptSource || built.ScriptSHA1() != identity.redisSHA1 || built.SourceSHA256() != identity.sourceSHA256 {
		t.Fatalf("%s selected script = %q/%q/%q, want %q/%q/%q",
			expectation.operation,
			built.ScriptName(), built.ScriptSHA1(), built.SourceSHA256(),
			expectation.scriptSource, identity.redisSHA1, identity.sourceSHA256,
		)
	}
}

func wireOracleAssertSemanticAndTail(
	t *testing.T,
	fixture *wireOracleFixture,
	expectation wireOracleOperationExpectation,
	variant wireOracleGateVariant,
	prefixCount int,
	request OperationWireRequest,
	built EvalSHARequest,
) {
	t.Helper()
	if len(request.semantic) != len(expectation.semanticFields) {
		t.Fatalf("%s semantic field count = %d, want %d", expectation.operation, len(request.semantic), len(expectation.semanticFields))
	}
	arguments := built.Arguments()
	if len(arguments) < prefixCount+len(expectation.semanticFields) {
		t.Fatalf("%s arguments ended before all semantic fields", expectation.operation)
	}
	for index, wantName := range expectation.semanticFields {
		field := request.semantic[index]
		if field.Name != wantName {
			t.Fatalf("%s semantic field %d = %q, want %q", expectation.operation, index, field.Name, wantName)
		}
		if !bytes.Equal(arguments[prefixCount+index], field.Value) {
			t.Fatalf("%s semantic field %q was not bound at argument %d", expectation.operation, wantName, prefixCount+index)
		}
		if wantValue, targeted := wireOracleTargetedSemanticValue(fixture, expectation.operation, variant, wantName); targeted &&
			(string(field.Value) != wantValue || string(arguments[prefixCount+index]) != wantValue) {
			t.Fatalf("%s semantic field %q = request:%q argument:%q, want fixture-derived %q",
				expectation.operation, wantName, field.Value, arguments[prefixCount+index], wantValue)
		}
	}

	tailOffset := prefixCount + len(expectation.semanticFields)
	switch expectation.tailKind {
	case wireOracleNoTail:
		if len(request.records) != 0 || len(request.recordBytes) != 0 || len(request.repeated) != 0 || len(arguments) != tailOffset {
			t.Fatalf("%s unexpected tail: records=%d encoded=%d repeated=%d arguments=%d want=%d",
				expectation.operation, len(request.records), len(request.recordBytes), len(request.repeated), len(arguments), tailOffset)
		}
	case wireOracleRecordTail:
		if len(request.records) != expectation.tailCount || len(request.recordBytes) != expectation.tailCount || len(request.repeated) != 0 {
			t.Fatalf("%s record linkage = records:%d encoded:%d repeated:%d, want records/encoded:%d repeated:0",
				expectation.operation, len(request.records), len(request.recordBytes), len(request.repeated), expectation.tailCount)
		}
		wireOracleAssertCountField(t, expectation, request, prefixCount, arguments, len(request.records))
		if len(arguments) != tailOffset+len(request.records) {
			t.Fatalf("%s argument count = %d, want %d including %d records", expectation.operation, len(arguments), tailOffset+len(request.records), len(request.records))
		}
		for recordIndex, record := range request.records {
			wireOracleAssertRecordFields(t, fixture, expectation.operation, variant, recordIndex, record, expectation.recordFields)
			encoded, err := EncodeRecord(record)
			if err != nil {
				t.Fatalf("%s encode record %d: %v", expectation.operation, recordIndex, err)
			}
			if !bytes.Equal(request.recordBytes[recordIndex], encoded) || !bytes.Equal(arguments[tailOffset+recordIndex], encoded) {
				t.Fatalf("%s record %d is not linked to its encoded wire argument", expectation.operation, recordIndex)
			}
		}
	case wireOracleActiveRunIDTail:
		if len(request.records) != 0 || len(request.recordBytes) != 0 || len(request.repeated) != expectation.tailCount {
			t.Fatalf("%s active-run linkage = records:%d encoded:%d repeated:%d, want 0/0/%d",
				expectation.operation, len(request.records), len(request.recordBytes), len(request.repeated), expectation.tailCount)
		}
		wireOracleAssertCountField(t, expectation, request, prefixCount, arguments, len(request.repeated))
		if len(arguments) != tailOffset+len(request.repeated) {
			t.Fatalf("%s argument count = %d, want %d including active run IDs", expectation.operation, len(arguments), tailOffset+len(request.repeated))
		}
		wantRunIDs := []RunID{fixture.runID, fixture.otherRunID}
		for index, wantRunID := range wantRunIDs {
			if string(request.repeated[index]) != string(wantRunID) || !bytes.Equal(arguments[tailOffset+index], request.repeated[index]) {
				t.Fatalf("%s active run ID %d = %q, want %q at the same wire position", expectation.operation, index, request.repeated[index], wantRunID)
			}
		}
	default:
		t.Fatalf("%s has unknown oracle tail kind %d", expectation.operation, expectation.tailKind)
	}
}

// wireOracleTargetedSemanticValue keeps security-relevant same-shaped values
// independent of request.semantic. This makes the read-only oracle detect, for
// example, a constructor swapping run/owner IDs or commit/output digests even
// when the production request and serializer agree with each other.
func wireOracleTargetedSemanticValue(
	fixture *wireOracleFixture,
	operation OperationName,
	variant wireOracleGateVariant,
	field string,
) (string, bool) {
	if operationCanaries := fixture.semanticCanaries[operation]; operationCanaries != nil {
		if value, exists := operationCanaries[field]; exists {
			return value, true
		}
	}
	switch field {
	case "run_id":
		return string(fixture.runID), true
	case "job_id":
		return string(fixture.job.JobID), true
	case "owner_id":
		return string(fixture.lease.OwnerID), true
	case "rate_scope_id", "job_rate_scope_id":
		return string(fixture.job.RateScopeID), true
	case "lease_token":
		return string(fixture.lease.Token), true
	case "fence":
		return strconv.FormatUint(uint64(fixture.lease.Fence), 10), true
	case "reservation_id":
		return string(fixture.reservationID), true
	case "commit_id", "expected_commit_id":
		return string(fixture.commitID), true
	case "publication_id":
		return string(fixture.publicationID), true
	case "output_digest", "verified_output_digest":
		return string(fixture.outputDigest), true
	case "verified_manifest_chunk_digest":
		return string(fixture.manifestChunkDigest), true
	case "contract_sha256":
		return string(fixture.artifacts.contract), true
	case "source_sha256":
		return string(fixture.sourceSHA256), true
	case "authorization_sha256":
		return string(fixture.authorizationSHA256), true
	case "authorization_scope_sha256":
		return string(fixture.authorizationScopeSHA256), true
	case "canonicalization_sha256":
		return string(fixture.canonicalizationSHA256), true
	case "crawl_policy_sha256":
		return string(fixture.crawlPolicySHA256), true
	case "render_policy_sha256":
		return string(fixture.renderPolicySHA256), true
	case "archive_sha256":
		return string(fixture.archiveSHA256), true
	case "evidence_sha256":
		if operation == OperationApproveBoot {
			return string(fixture.bootEvidenceSHA256), true
		}
		if operation == OperationPurgeRunBatch {
			return string(fixture.purgeEvidenceSHA256), true
		}
	case "planned_shutdown_evidence_sha256_or_empty":
		return string(fixture.plannedShutdownEvidenceSHA256), true
	case "process_stop_evidence_sha256":
		if operation == OperationInstallCandidateMarkers {
			return string(fixture.installProcessStopEvidenceSHA256), true
		}
		if operation == OperationMarkPlannedShutdown {
			return string(fixture.shutdownProcessStopEvidenceSHA256), true
		}
	case "policy_group_map_sha256":
		return string(fixture.policyGroupMap), true
	case "expected_first_job_id_or_empty":
		return string(fixture.job.JobID), true
	case "rank_offset":
		return strconv.FormatUint(fixture.rankOffset, 10), true
	case "expected_cleanup_due_at_ms":
		return strconv.FormatUint(fixture.cleanupDueAtMS, 10), true
	case "source_kind":
		if variant == wireOracleCandidateBeforeRetirement {
			return string(SourceV1Migration), true
		}
		return string(SourceMongo), true
	case "reason":
		switch operation {
		case OperationRejectReady:
			return string(ReasonPolicyDenied), true
		case OperationRetry:
			return string(ReasonRequestTimeout), true
		case OperationDead:
			return string(ReasonHTTP4xx), true
		case OperationCancelJob:
			return string(ReasonOperatorCancelled), true
		case OperationCompleteNoOutput:
			return string(ReasonAlreadyVisited), true
		case OperationCancelRun:
			return string(ReasonSourceCancelled), true
		}
	}
	return "", false
}

func wireOracleAssertCountField(
	t *testing.T,
	expectation wireOracleOperationExpectation,
	request OperationWireRequest,
	prefixCount int,
	arguments [][]byte,
	count int,
) {
	t.Helper()
	index := -1
	for candidate, name := range expectation.semanticFields {
		if name == expectation.countField {
			index = candidate
			break
		}
	}
	if index < 0 {
		t.Fatalf("%s count field %q is absent from the literal semantic plan", expectation.operation, expectation.countField)
	}
	want := strconv.Itoa(count)
	if string(request.semantic[index].Value) != want || string(arguments[prefixCount+index]) != want {
		t.Fatalf("%s %s = semantic:%q argument:%q, want linked count %q",
			expectation.operation, expectation.countField, request.semantic[index].Value, arguments[prefixCount+index], want)
	}
}

func wireOracleAssertRecordFields(
	t *testing.T,
	fixture *wireOracleFixture,
	operation OperationName,
	variant wireOracleGateVariant,
	recordIndex int,
	record Record,
	want []string,
) {
	t.Helper()
	if len(record) != len(want) {
		t.Fatalf("%s record %d field count = %d, want %d", operation, recordIndex, len(record), len(want))
	}
	for fieldIndex, name := range want {
		if record[fieldIndex].Name != name {
			t.Fatalf("%s record %d field %d = %q, want %q", operation, recordIndex, fieldIndex, record[fieldIndex].Name, name)
		}
		if value, targeted := wireOracleTargetedRecordValue(fixture, operation, variant, name); targeted && string(record[fieldIndex].Value) != value {
			t.Fatalf("%s record %d field %q = %q, want fixture-derived %q", operation, recordIndex, name, record[fieldIndex].Value, value)
		}
	}
}

func wireOracleTargetedRecordValue(
	fixture *wireOracleFixture,
	operation OperationName,
	variant wireOracleGateVariant,
	field string,
) (string, bool) {
	switch field {
	case "group_scope_id", "initial_origin_scope_id", "policy_decision_sha256", "render_policy_sha256", "publication_id":
		return wireOracleTargetedSemanticValue(fixture, operation, variant, field)
	default:
		return "", false
	}
}

func wireOracleAssertKeys(
	t *testing.T,
	fixture *wireOracleFixture,
	expectation wireOracleOperationExpectation,
	request OperationWireRequest,
	built EvalSHARequest,
) {
	t.Helper()
	want := wireOracleExpectedKeys(fixture, expectation)
	wireOracleAssertByteStrings(t, string(expectation.operation)+" constructor keys", request.keys, want)
	wireOracleAssertByteStrings(t, string(expectation.operation)+" built keys", built.Keys(), want)
}

func wireOracleAssertSize(t *testing.T, expectation wireOracleOperationExpectation, built EvalSHARequest) {
	t.Helper()
	wantSize := wireOracleRESPSize(built.ScriptSHA1(), built.Keys(), built.Arguments())
	if built.SerializedSize() != wantSize {
		t.Fatalf("%s serialized size = %d, independent RESP size = %d", expectation.operation, built.SerializedSize(), wantSize)
	}
	if built.SerializedSize() > expectation.requestLimit {
		t.Fatalf("%s serialized size = %d, exceeds normative class limit %d", expectation.operation, built.SerializedSize(), expectation.requestLimit)
	}
}

func wireOracleRESPSize(scriptSHA1 string, keys, arguments [][]byte) uint64 {
	partCount := 3 + len(keys) + len(arguments)
	size := uint64(1 + len(strconv.Itoa(partCount)) + 2)
	addBulk := func(value []byte) {
		length := len(value)
		size += uint64(1 + len(strconv.Itoa(length)) + 2 + length + 2)
	}
	addBulk([]byte("EVALSHA"))
	addBulk([]byte(scriptSHA1))
	addBulk([]byte(strconv.Itoa(len(keys))))
	for _, value := range keys {
		addBulk(value)
	}
	for _, value := range arguments {
		addBulk(value)
	}
	return size
}

func wireOracleAssertByteStrings(t *testing.T, label string, got [][]byte, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s count = %d, want %d", label, len(got), len(want))
	}
	for index, value := range want {
		if string(got[index]) != value {
			t.Fatalf("%s[%d] = %q, want %q", label, index, got[index], value)
		}
	}
}

func wireOracleAssertOperationNames(t *testing.T, label string, got, want []OperationName) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s count = %d, want %d", label, len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("%s[%d] = %s, want %s", label, index, got[index], want[index])
		}
	}
}

func wireOracleOperationExpectations() []wireOracleOperationExpectation {
	return []wireOracleOperationExpectation{
		{
			operation:    OperationApproveBoot,
			scriptSource: "cj2_approve_boot.lua",
			semanticFields: []string{
				"current_redis_run_id", "proposed_boot_epoch", "evidence_sha256", "evidence_at_ms", "loss_bound",
				"planned_nonce_or_empty", "planned_shutdown_evidence_sha256_or_empty", "approval_mode",
			},
			tailKind: wireOracleNoTail, keyPlan: wireOracleKeysApproveBoot,
			requestLimit: MaxOrdinaryEvalSHARequestBytes, variants: []wireOracleGateVariant{wireOracleUngated},
		},
		{
			operation:    OperationInstallCandidateMarkers,
			scriptSource: "cj2_install_candidate_markers.lua",
			semanticFields: []string{
				"freeze_nonce", "process_stop_evidence_sha256", "contract_sha256",
				"manifest_version", "manifest_sha256", "crawl_jobs", "crawl_policy", "canonicalization", "page_publication",
				"image_manifest", "backlink_projection", "render_ipc", "signal_queue", "global_request_concurrency",
				"redis_config_sha256", "commit_guard_sha256", "spider_image", "seed_importer_image", "crawl_admin_image",
				"indexer_image", "image_indexer_image", "backlinks_processor_image", "monitoring_image", "render_worker_image",
			},
			tailKind: wireOracleNoTail, keyPlan: wireOracleKeysAuthority,
			requestLimit: MaxOrdinaryEvalSHARequestBytes, variants: []wireOracleGateVariant{wireOracleBootOnly},
		},
		{
			operation:    OperationRetireLegacyKeys,
			scriptSource: "cj2_retire_legacy_keys.lua",
			semanticFields: []string{
				"freeze_nonce", "backup_sha256", "v1_count", "v1_url_field_count", "v1_depth_field_count",
				"v1_source_sha256", "v1_queue_evidence_sha256", "v1_urls_evidence_sha256", "v1_depths_evidence_sha256",
				"spider_queue_type", "spider_queue_count", "spider_queue_evidence_sha256", "signal_queue_type",
				"signal_queue_count", "signal_queue_evidence_sha256", "confirmation_text",
			},
			tailKind: wireOracleNoTail, keyPlan: wireOracleKeysRetire,
			requestLimit: MaxOrdinaryEvalSHARequestBytes,
			variants:     []wireOracleGateVariant{wireOracleCandidateBeforeRetirement, wireOracleCandidateAfterRetirement},
		},
		{
			operation:    OperationPromoteCandidateContracts,
			scriptSource: "cj2_promote_candidate_contracts.lua",
			semanticFields: []string{
				"freeze_nonce", "commit_guard_sha256", "protocol_version", "contract_sha256", "redis_version",
				"redis_config_sha256", "maximum_shape_sha256", "memory_fixture_sha256", "lua_benchmark_sha256",
				"aof_crash_evidence_sha256", "cutover_mode", "candidate_run_id", "approved",
			},
			tailKind: wireOracleNoTail, keyPlan: wireOracleKeysPromote,
			requestLimit: MaxOrdinaryEvalSHARequestBytes, variants: []wireOracleGateVariant{wireOracleCandidateAfterRetirement},
		},
		{
			operation:    OperationMarkPlannedShutdown,
			scriptSource: "cj2_mark_planned_shutdown.lua",
			semanticFields: []string{
				"planned_shutdown_nonce", "process_stop_evidence_sha256", "active_run_count",
			},
			tailKind: wireOracleActiveRunIDTail, tailCount: 2, countField: "active_run_count", keyPlan: wireOracleKeysMarkShutdown,
			requestLimit: MaxOrdinaryEvalSHARequestBytes, variants: []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:    OperationCreateRun,
			scriptSource: "cj2_create_run.lua",
			semanticFields: []string{
				"run_id", "source_kind", "source_sha256", "expected_seed_count", "authorization_sha256",
				"authorization_scope_sha256", "authorization_expires_at_ms", "canonicalization_version",
				"canonicalization_sha256", "crawl_policy_version", "crawl_policy_sha256", "render_policy_version",
				"render_policy_sha256", "policy_group_map_sha256", "max_jobs", "max_request_starts",
				"global_concurrency_limit", "max_delivery_attempts", "policy_group_count",
			},
			recordFields: []string{
				"group_id", "rate_scope_id", "group_scope_id", "request_start_limit", "concurrency", "interval_ms",
			},
			tailKind: wireOracleRecordTail, tailCount: 1, countField: "policy_group_count", keyPlan: wireOracleKeysCreateRun,
			requestLimit: MaxOrdinaryEvalSHARequestBytes,
			variants:     []wireOracleGateVariant{wireOracleActive, wireOracleCandidateBeforeRetirement},
		},
		{
			operation:      OperationEnqueueBatch,
			scriptSource:   "cj2_enqueue_batch.lua",
			semanticFields: []string{"run_id", "record_count"},
			recordFields: []string{
				"job_id", "canonical_url", "score_text", "depth", "group_id", "rate_scope_id", "group_scope_id",
				"initial_origin_scope_id", "policy_decision_sha256",
			},
			tailKind: wireOracleRecordTail, tailCount: 1, countField: "record_count", keyPlan: wireOracleKeysRunRecords,
			requestLimit: MaxOrdinaryEvalSHARequestBytes,
			variants:     []wireOracleGateVariant{wireOracleActive, wireOracleCandidateBeforeRetirement},
		},
		{
			operation:      OperationBeginRunAudit,
			scriptSource:   "cj2_begin_run_audit.lua",
			semanticFields: []string{"run_id"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysRun,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive, wireOracleCandidateBeforeRetirement},
		},
		{
			operation:      OperationAuditRunBatch,
			scriptSource:   "cj2_audit_run_batch.lua",
			semanticFields: []string{"run_id", "expected_prior_cursor", "expected_prior_count", "record_count"},
			recordFields: []string{
				"job_id", "canonical_url", "score_text", "depth", "group_id", "rate_scope_id", "group_scope_id",
				"initial_origin_scope_id", "policy_decision_sha256",
			},
			tailKind: wireOracleRecordTail, tailCount: 1, countField: "record_count", keyPlan: wireOracleKeysRunRecords,
			requestLimit: MaxOrdinaryEvalSHARequestBytes,
			variants:     []wireOracleGateVariant{wireOracleActive, wireOracleCandidateBeforeRetirement},
		},
		{
			operation:      OperationSealRun,
			scriptSource:   "cj2_seal_run.lua",
			semanticFields: []string{"run_id", "expected_job_count", "source_sha256"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysRun,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive, wireOracleCandidateBeforeRetirement},
		},
		{
			operation:    OperationActivateRun,
			scriptSource: "cj2_activate_run.lua",
			semanticFields: []string{
				"run_id", "confirmation_text", "authorization_sha256", "crawl_policy_sha256", "render_policy_sha256",
				"canonicalization_sha256",
			},
			tailKind: wireOracleNoTail, keyPlan: wireOracleKeysRun,
			requestLimit: MaxOrdinaryEvalSHARequestBytes, variants: []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:    OperationRejectReady,
			scriptSource: "cj2_reject_ready.lua",
			semanticFields: []string{
				"run_id", "job_id", "canonical_url", "score_text", "depth", "group_id", "rate_scope_id",
				"group_scope_id", "initial_origin_scope_id", "policy_decision_sha256", "reason", "transition_id",
			},
			tailKind: wireOracleNoTail, keyPlan: wireOracleKeysJob,
			requestLimit: MaxOrdinaryEvalSHARequestBytes, variants: []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:    OperationTryClaim,
			scriptSource: "cj2_try_claim.lua",
			semanticFields: []string{
				"run_id", "job_id", "canonical_url", "score_text", "depth", "job_group_id", "job_rate_scope_id",
				"job_group_scope_id", "job_initial_origin_scope_id", "job_policy_decision_sha256", "expected_prior_fence",
				"fence", "owner_id", "lease_token", "request_ordinal", "request_kind", "target_url_id",
				"canonical_target_url", "target_digest", "crawl_policy_sha256", "policy_decision_sha256", "group_id",
				"rate_scope_id", "global_scope_id", "group_scope_id", "origin_scope_id", "global_concurrency",
				"global_interval_ms", "group_concurrency", "group_interval_ms", "origin_concurrency", "origin_interval_ms",
				"transition_id",
			},
			tailKind: wireOracleNoTail, keyPlan: wireOracleKeysReservation,
			requestLimit: MaxOrdinaryEvalSHARequestBytes, variants: []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:      OperationRenewLease,
			scriptSource:   "cj2_renew_lease.lua",
			semanticFields: []string{"run_id", "job_id", "owner_id", "lease_token", "fence"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysJob,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:    OperationReserveRequest,
			scriptSource: "cj2_reserve_request.lua",
			semanticFields: []string{
				"run_id", "job_id", "owner_id", "lease_token", "fence", "request_ordinal", "request_kind",
				"target_url_id", "canonical_target_url", "target_digest", "crawl_policy_sha256", "policy_decision_sha256",
				"group_id", "rate_scope_id", "global_scope_id", "group_scope_id", "origin_scope_id", "global_concurrency",
				"global_interval_ms", "group_concurrency", "group_interval_ms", "origin_concurrency", "origin_interval_ms",
			},
			tailKind: wireOracleNoTail, keyPlan: wireOracleKeysReservation,
			requestLimit: MaxOrdinaryEvalSHARequestBytes, variants: []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:      OperationStartRequest,
			scriptSource:   "cj2_start_request.lua",
			semanticFields: []string{"run_id", "job_id", "owner_id", "lease_token", "fence", "reservation_id"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysReservation,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:      OperationFinishRequest,
			scriptSource:   "cj2_finish_request.lua",
			semanticFields: []string{"run_id", "job_id", "owner_id", "lease_token", "fence", "reservation_id"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysReservation,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:      OperationCancelReservation,
			scriptSource:   "cj2_cancel_reservation.lua",
			semanticFields: []string{"run_id", "job_id", "owner_id", "lease_token", "fence", "reservation_id"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysReservation,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:      OperationReleaseBeforeIO,
			scriptSource:   "cj2_release_before_io.lua",
			semanticFields: []string{"run_id", "job_id", "owner_id", "lease_token", "fence", "transition_id"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysJob,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:      OperationRetry,
			scriptSource:   "cj2_retry.lua",
			semanticFields: []string{"run_id", "job_id", "owner_id", "lease_token", "fence", "reason", "transition_id"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysJob,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:      OperationDead,
			scriptSource:   "cj2_dead.lua",
			semanticFields: []string{"run_id", "job_id", "owner_id", "lease_token", "fence", "reason", "transition_id"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysJob,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:      OperationCancelJob,
			scriptSource:   "cj2_cancel_job.lua",
			semanticFields: []string{"run_id", "job_id", "owner_id", "lease_token", "fence", "reason", "transition_id"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysJob,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:      OperationCompleteNoOutput,
			scriptSource:   "cj2_complete_no_output.lua",
			semanticFields: []string{"run_id", "job_id", "owner_id", "lease_token", "fence", "reason", "transition_id"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysJob,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:    OperationBeginStage,
			scriptSource: "cj2_begin_stage.lua",
			semanticFields: []string{
				"run_id", "job_id", "owner_id", "lease_token", "fence", "commit_id", "publication_id", "output_digest",
				"request_starts_baseline", "request_starts_generation",
				"expected_page_fields", "expected_outlinks", "expected_discoveries", "expected_aliases", "expected_images",
			},
			tailKind: wireOracleNoTail, keyPlan: wireOracleKeysStage,
			requestLimit: MaxOrdinaryEvalSHARequestBytes, variants: []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:    OperationStagePageFields,
			scriptSource: "cj2_stage_page_fields.lua",
			semanticFields: []string{
				"run_id", "job_id", "owner_id", "lease_token", "fence", "commit_id", "chunk_kind", "chunk_ordinal",
				"chunk_digest", "record_count",
			},
			recordFields: []string{
				"normalized_url", "content_type", "status_code", "last_crawled", "rendered", "render_policy_rule",
				"render_policy_sha256", "publication_id",
			},
			tailKind: wireOracleRecordTail, tailCount: 1, countField: "record_count", keyPlan: wireOracleKeysStage,
			requestLimit: MaxNonBlobStageBatchRequestBytes, variants: []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:    OperationStagePageBlob,
			scriptSource: "cj2_stage_page_blob.lua",
			semanticFields: []string{
				"run_id", "job_id", "owner_id", "lease_token", "fence", "commit_id", "chunk_kind", "chunk_ordinal",
				"chunk_digest", "record_count",
			},
			recordFields: []string{"field_name", "field_bytes"},
			tailKind:     wireOracleRecordTail, tailCount: 1, countField: "record_count", keyPlan: wireOracleKeysStage,
			requestLimit: MaxPageBlobEvalSHARequestBytes, variants: []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:    OperationStageOutlinksBatch,
			scriptSource: "cj2_stage_outlinks_batch.lua",
			semanticFields: []string{
				"run_id", "job_id", "owner_id", "lease_token", "fence", "commit_id", "chunk_kind", "chunk_ordinal",
				"chunk_digest", "record_count",
			},
			recordFields: []string{"target_url"},
			tailKind:     wireOracleRecordTail, tailCount: 1, countField: "record_count", keyPlan: wireOracleKeysStage,
			requestLimit: MaxNonBlobStageBatchRequestBytes, variants: []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:    OperationStageDiscoveriesBatch,
			scriptSource: "cj2_stage_discoveries_batch.lua",
			semanticFields: []string{
				"run_id", "job_id", "owner_id", "lease_token", "fence", "commit_id", "chunk_kind", "chunk_ordinal",
				"chunk_digest", "record_count",
			},
			recordFields: []string{
				"job_id", "canonical_url", "depth", "score_text", "group_id", "rate_scope_id", "group_scope_id",
				"initial_origin_scope_id", "policy_decision_sha256",
			},
			tailKind: wireOracleRecordTail, tailCount: 1, countField: "record_count", keyPlan: wireOracleKeysStage,
			requestLimit: MaxNonBlobStageBatchRequestBytes, variants: []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:    OperationStageAliasesBatch,
			scriptSource: "cj2_stage_aliases_batch.lua",
			semanticFields: []string{
				"run_id", "job_id", "owner_id", "lease_token", "fence", "commit_id", "chunk_kind", "chunk_ordinal",
				"chunk_digest", "record_count",
			},
			recordFields: []string{"url_id", "canonical_url", "depth"},
			tailKind:     wireOracleRecordTail, tailCount: 1, countField: "record_count", keyPlan: wireOracleKeysStage,
			requestLimit: MaxNonBlobStageBatchRequestBytes, variants: []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:    OperationStageImagesBatch,
			scriptSource: "cj2_stage_images_batch.lua",
			semanticFields: []string{
				"run_id", "job_id", "owner_id", "lease_token", "fence", "commit_id", "chunk_kind", "chunk_ordinal",
				"chunk_digest", "record_count",
			},
			recordFields: []string{"normalized_source_url", "alt"},
			tailKind:     wireOracleRecordTail, tailCount: 1, countField: "record_count", keyPlan: wireOracleKeysStage,
			requestLimit: MaxNonBlobStageBatchRequestBytes, variants: []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:    OperationStageImageManifest,
			scriptSource: "cj2_stage_image_manifest.lua",
			semanticFields: []string{
				"run_id", "job_id", "owner_id", "lease_token", "fence", "commit_id", "chunk_kind", "chunk_ordinal",
				"chunk_digest", "record_count",
			},
			recordFields: []string{"contract_version", "publication_id", "normalized_url", "image_count", "image_keys"},
			tailKind:     wireOracleRecordTail, tailCount: 1, countField: "record_count", keyPlan: wireOracleKeysStage,
			requestLimit: MaxNonBlobStageBatchRequestBytes, variants: []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:      OperationAbortStage,
			scriptSource:   "cj2_abort_stage.lua",
			semanticFields: []string{"run_id", "job_id", "owner_id", "lease_token", "fence", "commit_id", "transition_id"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysStage,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:    OperationSealStage,
			scriptSource: "cj2_seal_stage.lua",
			semanticFields: []string{
				"run_id", "job_id", "owner_id", "lease_token", "fence", "commit_id", "verified_output_digest",
				"verified_manifest_chunk_digest",
			},
			tailKind: wireOracleNoTail, keyPlan: wireOracleKeysStage,
			requestLimit: MaxOrdinaryEvalSHARequestBytes, variants: []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:      OperationCommit,
			scriptSource:   "cj2_commit.lua",
			semanticFields: []string{"run_id", "job_id", "owner_id", "lease_token", "fence", "commit_id"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysStage,
			requestLimit:   MaxCommitEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:      OperationPromoteDue,
			scriptSource:   "cj2_promote_due.lua",
			semanticFields: []string{"run_id"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysRunMaintenance,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:      OperationRecoverExpired,
			scriptSource:   "cj2_recover_expired.lua",
			semanticFields: []string{"run_id"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysRunMaintenance,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:      OperationCancelRun,
			scriptSource:   "cj2_cancel_run.lua",
			semanticFields: []string{"run_id", "reason"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysRun,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive, wireOracleCandidateBeforeRetirement},
		},
		{
			operation:      OperationCancelBatch,
			scriptSource:   "cj2_cancel_batch.lua",
			semanticFields: []string{"run_id"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysRunMaintenance,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive, wireOracleCandidateBeforeRetirement},
		},
		{
			operation:      OperationFinalizeRun,
			scriptSource:   "cj2_finalize_run.lua",
			semanticFields: []string{"run_id"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysRunMaintenance,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:      OperationArchiveRun,
			scriptSource:   "cj2_archive_run.lua",
			semanticFields: []string{"run_id", "archive_sha256", "confirmation_text"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysArchive,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:      OperationPurgeRunBatch,
			scriptSource:   "cj2_purge_run_batch.lua",
			semanticFields: []string{"run_id", "evidence_sha256", "expected_first_job_id_or_empty"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysPurge,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive, wireOracleCandidateBeforeRetirement},
		},
		{
			operation:      OperationCleanStage,
			scriptSource:   "cj2_clean_stage.lua",
			semanticFields: []string{"expected_commit_id", "expected_cleanup_due_at_ms"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysCleanStage,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive},
		},
		{
			operation:      OperationMaintainRateScopes,
			scriptSource:   "cj2_maintain_rate_scopes.lua",
			semanticFields: []string{"rank_offset"},
			tailKind:       wireOracleNoTail,
			keyPlan:        wireOracleKeysRateMaintenance,
			requestLimit:   MaxOrdinaryEvalSHARequestBytes,
			variants:       []wireOracleGateVariant{wireOracleActive},
		},
	}
}

type wireOracleArtifacts struct {
	bootEpoch string
	contract  Digest
	marker    CompatibilityMarker
	guard     StoredCommitGuard
	legacy    LegacyRetirementRecord
	freeze    AdminFreezeRecord
	core      GuardCore
}

type wireOracleFixture struct {
	runID                             RunID
	otherRunID                        RunID
	bootEvidenceSHA256                Digest
	plannedShutdownEvidenceSHA256     Digest
	installProcessStopEvidenceSHA256  Digest
	shutdownProcessStopEvidenceSHA256 Digest
	sourceSHA256                      Digest
	authorizationSHA256               Digest
	authorizationScopeSHA256          Digest
	canonicalizationSHA256            Digest
	crawlPolicySHA256                 Digest
	renderPolicySHA256                Digest
	archiveSHA256                     Digest
	purgeEvidenceSHA256               Digest
	policyGroup                       PolicyGroup
	policyGroupMap                    Digest
	runPolicy                         RunPolicyAuthority
	job                               SourceJob
	discoveryDecision                 PolicyDecision
	lease                             LeaseIdentity
	intent                            ReservationIntent
	reservationID                     ReservationID
	outputDigest                      Digest
	publicationID                     Digest
	commitIdentity                    CommitIdentity
	outputContext                     OutputContext
	output                            CrawlOutput
	commitID                          Digest
	manifestChunkDigest               Digest
	rankOffset                        uint64
	cleanupDueAtMS                    uint64
	retireInput                       RetireLegacyKeysWireInput
	chunks                            map[OperationName]StageChunk
	semanticCanaries                  map[OperationName]map[string]string
	artifacts                         wireOracleArtifacts
}

func newWireOracleScriptBindingSet(
	t *testing.T,
	expectations []wireOracleOperationExpectation,
	approvedContractSHA256 Digest,
) (ScriptBindingSet, map[OperationName]wireOracleScriptIdentity) {
	t.Helper()
	sources := make([]testScriptSource, 0, len(expectations))
	identities := make(map[OperationName]wireOracleScriptIdentity, len(expectations))
	for _, expectation := range expectations {
		source := []byte("-- independent constructor oracle: " + expectation.scriptSource + "\nreturn {'" + string(expectation.operation) + "'}\n")
		redisDigest := sha1.Sum(source) // #nosec G401 -- Redis EVALSHA identity is normatively SHA-1.
		sourceDigest := sha256.Sum256(source)
		identity := wireOracleScriptIdentity{
			source:       append([]byte(nil), source...),
			redisSHA1:    hex.EncodeToString(redisDigest[:]),
			sourceSHA256: Digest(hex.EncodeToString(sourceDigest[:])),
		}
		identities[expectation.operation] = identity
		sources = append(sources, testScriptSource{
			operation:  expectation.operation,
			sourceName: expectation.scriptSource,
			source:     append([]byte(nil), source...),
		})
	}
	bindings := newTestScriptBindingSetFromSources(t, sources, approvedContractSHA256)
	if len(bindings.bindings) != wireOracleConstructorCount {
		t.Fatalf("complete ScriptBindingSet contains %d bindings, want %d", len(bindings.bindings), wireOracleConstructorCount)
	}
	for index, expectation := range expectations {
		identity := identities[expectation.operation]
		binding := bindings.bindings[index]
		if binding.operation != expectation.operation || binding.sourceName == "" || !bytes.Equal([]byte(binding.source), identity.source) {
			t.Fatalf("complete ScriptBindingSet omitted exact source for %s", expectation.operation)
		}
	}
	return bindings, identities
}

func wireOracleVariantName(variant wireOracleGateVariant) string {
	switch variant {
	case wireOracleUngated:
		return "ungated"
	case wireOracleBootOnly:
		return "boot_only"
	case wireOracleActive:
		return "active"
	case wireOracleCandidateBeforeRetirement:
		return "candidate_before_legacy_retirement"
	case wireOracleCandidateAfterRetirement:
		return "candidate_after_legacy_retirement"
	default:
		return "unknown"
	}
}

func wireOracleGateShape(variant wireOracleGateVariant) (GateMode, CandidatePhase, [7]bool) {
	switch variant {
	case wireOracleBootOnly:
		return GateBootOnly, "", [7]bool{true, true, false, false, false, false, false}
	case wireOracleActive:
		return GateActive, "", [7]bool{true, true, true, true, true, true, false}
	case wireOracleCandidateBeforeRetirement:
		return GateCandidate, CandidateBeforeLegacyRetirement, [7]bool{true, true, true, true, false, false, true}
	case wireOracleCandidateAfterRetirement:
		return GateCandidate, CandidateAfterLegacyRetirement, [7]bool{true, true, true, true, false, true, true}
	default:
		return "", "", [7]bool{}
	}
}

func wireOracleGate(
	t *testing.T,
	fixture *wireOracleFixture,
	operation OperationName,
	variant wireOracleGateVariant,
) TransportGate {
	t.Helper()
	if variant == wireOracleUngated {
		return TransportGate{}
	}
	mode, phase, _ := wireOracleGateShape(variant)
	input := TransportGateInput{Mode: mode, CandidatePhase: phase, BootEpoch: fixture.artifacts.bootEpoch}
	switch variant {
	case wireOracleBootOnly:
	case wireOracleActive:
		input.Contract = fixture.artifacts.contract
		input.Compatibility = &fixture.artifacts.marker
		input.CommitGuard = &fixture.artifacts.guard
		input.Legacy = &fixture.artifacts.legacy
	case wireOracleCandidateBeforeRetirement, wireOracleCandidateAfterRetirement:
		input.Contract = fixture.artifacts.contract
		input.Compatibility = &fixture.artifacts.marker
		input.AdminFreeze = &fixture.artifacts.freeze
		if variant == wireOracleCandidateAfterRetirement {
			input.Legacy = &fixture.artifacts.legacy
		}
	default:
		t.Fatalf("unknown gate variant %d", variant)
	}
	gate, err := NewTransportGate(operation, input)
	if err != nil {
		t.Fatalf("construct %s %s gate: %v", operation, wireOracleVariantName(variant), err)
	}
	return gate
}

func wireOracleConstructRequest(
	fixture *wireOracleFixture,
	operation OperationName,
	variant wireOracleGateVariant,
	gate TransportGate,
) (OperationWireRequest, error) {
	switch operation {
	case OperationApproveBoot:
		return NewApproveBootWireRequest(ApproveBootWireInput{
			CurrentRedisRunID:             strings.Repeat("e", 40),
			ProposedBootEpoch:             strings.Repeat("f", 32),
			EvidenceSHA256:                fixture.bootEvidenceSHA256,
			EvidenceAtMS:                  11,
			PlannedNonce:                  strings.Repeat("b", 32),
			PlannedShutdownEvidenceSHA256: fixture.plannedShutdownEvidenceSHA256,
			ApprovalMode:                  ApprovalPlanned,
		})
	case OperationInstallCandidateMarkers:
		return NewInstallCandidateMarkersWireRequest(gate, InstallCandidateMarkersWireInput{
			FreezeNonce:               fixture.artifacts.freeze.FreezeNonce(),
			ProcessStopEvidenceSHA256: fixture.installProcessStopEvidenceSHA256,
			ContractSHA256:            fixture.artifacts.contract,
			Compatibility:             fixture.artifacts.marker,
		})
	case OperationRetireLegacyKeys:
		return NewRetireLegacyKeysWireRequest(gate, fixture.retireInput)
	case OperationPromoteCandidateContracts:
		return NewPromoteCandidateContractsWireRequest(gate, PromoteCandidateContractsWireInput{
			FreezeNonce: fixture.artifacts.freeze.FreezeNonce(),
			GuardCore:   fixture.artifacts.core,
		})
	case OperationMarkPlannedShutdown:
		return NewMarkPlannedShutdownWireRequest(gate, MarkPlannedShutdownWireInput{
			PlannedShutdownNonce:      strings.Repeat("9", 32),
			ProcessStopEvidenceSHA256: fixture.shutdownProcessStopEvidenceSHA256,
			ActiveRunIDs:              []RunID{fixture.otherRunID, fixture.runID},
		})
	case OperationCreateRun:
		sourceKind := SourceMongo
		if variant == wireOracleCandidateBeforeRetirement {
			sourceKind = SourceV1Migration
		}
		return NewCreateRunWireRequest(gate, CreateRunWireInput{
			RunID: fixture.runID, SourceKind: sourceKind, SourceSHA256: fixture.sourceSHA256, ExpectedSeedCount: 1,
			AuthorizationSHA256: fixture.authorizationSHA256, AuthorizationScopeSHA256: fixture.authorizationScopeSHA256, AuthorizationExpiresAtMS: 1_000,
			CanonicalizationVersion: 1, CanonicalizationSHA256: fixture.canonicalizationSHA256,
			CrawlPolicyVersion: 2, CrawlPolicySHA256: fixture.crawlPolicySHA256,
			RenderPolicyVersion: 1, RenderPolicySHA256: fixture.renderPolicySHA256,
			PolicyGroupMapSHA256: fixture.policyGroupMap, MaxJobs: MaxJobsPerRun,
			MaxRequestStarts: MaxRequestStartsPerRun, GlobalConcurrencyLimit: GlobalActiveRequestLimit,
			MaxDeliveryAttempts: MaxDeliveryAttempts, PolicyGroups: []PolicyGroup{fixture.policyGroup},
		})
	case OperationEnqueueBatch:
		return NewEnqueueBatchWireRequest(gate, fixture.runPolicy, fixture.runID, []SourceJob{fixture.job})
	case OperationBeginRunAudit:
		return NewBeginRunAuditWireRequest(gate, fixture.runID)
	case OperationAuditRunBatch:
		return NewAuditRunBatchWireRequest(gate, fixture.runPolicy, AuditRunBatchWireInput{
			RunID: fixture.runID, ExpectedPriorCursor: "", ExpectedPriorCount: 0, Jobs: []SourceJob{fixture.job},
		})
	case OperationSealRun:
		return NewSealRunWireRequest(gate, SealRunWireInput{
			RunID: fixture.runID, ExpectedJobCount: 1, SourceSHA256: fixture.sourceSHA256,
		})
	case OperationActivateRun:
		return NewActivateRunWireRequest(gate, ActivateRunWireInput{
			RunID: fixture.runID, SourceSHA256: fixture.sourceSHA256, AuthorizationSHA256: fixture.authorizationSHA256,
			CrawlPolicySHA256: fixture.crawlPolicySHA256, RenderPolicySHA256: fixture.renderPolicySHA256, CanonicalizationSHA256: fixture.canonicalizationSHA256,
		})
	case OperationRejectReady:
		return NewRejectReadyWireRequest(gate, fixture.runPolicy, RejectReadyTransitionInput{
			RunID: fixture.runID, Job: fixture.job, Reason: ReasonPolicyDenied,
		})
	case OperationTryClaim:
		return NewTryClaimWireRequest(gate, fixture.runPolicy, TryClaimTransitionInput{
			Job: fixture.job, Lease: fixture.lease, ExpectedPriorFence: 1, InitialIntent: fixture.intent,
		})
	case OperationRenewLease:
		return NewRenewLeaseWireRequest(gate, fixture.lease)
	case OperationReserveRequest:
		return NewReserveRequestWireRequest(gate, fixture.runPolicy, fixture.intent)
	case OperationStartRequest:
		return NewStartRequestWireRequest(gate, fixture.runPolicy, fixture.intent)
	case OperationFinishRequest:
		return NewFinishRequestWireRequest(gate, fixture.runPolicy, fixture.intent)
	case OperationCancelReservation:
		return NewCancelReservationWireRequest(gate, fixture.runPolicy, fixture.intent)
	case OperationReleaseBeforeIO:
		return NewReleaseBeforeIOWireRequest(gate, ReleaseBeforeIOTransitionInput{Lease: fixture.lease})
	case OperationRetry:
		return NewRetryWireRequest(gate, RetryTransitionInput{Lease: fixture.lease, Reason: ReasonRequestTimeout})
	case OperationDead:
		return NewDeadWireRequest(gate, DeadTransitionInput{Lease: fixture.lease, Reason: ReasonHTTP4xx})
	case OperationCancelJob:
		return NewCancelJobWireRequest(gate, CancelJobTransitionInput{Lease: fixture.lease, Reason: ReasonOperatorCancelled})
	case OperationCompleteNoOutput:
		return NewCompleteNoOutputWireRequest(gate, CompleteNoOutputTransitionInput{Lease: fixture.lease, Reason: ReasonAlreadyVisited})
	case OperationBeginStage:
		return NewBeginStageWireRequest(gate, BeginStageWireInput{
			Context: fixture.outputContext,
			Lease:   fixture.lease, Output: fixture.output,
		})
	case OperationStagePageFields:
		return NewStagePageFieldsWireRequest(gate, fixture.lease, fixture.chunks[operation])
	case OperationStagePageBlob:
		return NewStagePageBlobWireRequest(gate, fixture.lease, fixture.chunks[operation])
	case OperationStageOutlinksBatch:
		return NewStageOutlinksBatchWireRequest(gate, fixture.lease, fixture.chunks[operation])
	case OperationStageDiscoveriesBatch:
		return NewStageDiscoveriesBatchWireRequest(gate, fixture.lease, fixture.chunks[operation])
	case OperationStageAliasesBatch:
		return NewStageAliasesBatchWireRequest(gate, fixture.lease, fixture.chunks[operation])
	case OperationStageImagesBatch:
		return NewStageImagesBatchWireRequest(gate, fixture.lease, fixture.chunks[operation])
	case OperationStageImageManifest:
		return NewStageImageManifestWireRequest(gate, fixture.lease, fixture.chunks[operation])
	case OperationAbortStage:
		return NewAbortStageWireRequest(gate, AbortStageTransitionInput{Lease: fixture.lease, CommitID: fixture.commitID})
	case OperationSealStage:
		return NewSealStageWireRequest(gate, SealStageWireInput{
			Context: fixture.outputContext,
			Lease:   fixture.lease, CommitID: fixture.commitID, VerifiedOutputDigest: fixture.outputDigest,
			VerifiedManifestChunkDigest: fixture.manifestChunkDigest,
		})
	case OperationCommit:
		return NewCommitWireRequest(gate, fixture.commitIdentity)
	case OperationPromoteDue:
		return NewPromoteDueWireRequest(gate, fixture.runID)
	case OperationRecoverExpired:
		return NewRecoverExpiredWireRequest(gate, fixture.runID)
	case OperationCancelRun:
		return NewCancelRunWireRequest(gate, CancelRunWireInput{RunID: fixture.runID, Reason: ReasonSourceCancelled})
	case OperationCancelBatch:
		return NewCancelBatchWireRequest(gate, fixture.runID)
	case OperationFinalizeRun:
		return NewFinalizeRunWireRequest(gate, fixture.runID)
	case OperationArchiveRun:
		return NewArchiveRunWireRequest(gate, ArchiveRunWireInput{RunID: fixture.runID, ArchiveSHA256: fixture.archiveSHA256})
	case OperationPurgeRunBatch:
		return NewPurgeRunBatchWireRequest(gate, PurgeRunBatchWireInput{
			RunID: fixture.runID, EvidenceSHA256: fixture.purgeEvidenceSHA256, ExpectedFirstJobID: fixture.job.JobID,
		})
	case OperationCleanStage:
		return NewCleanStageWireRequest(gate, CleanStageWireInput{
			ExpectedCommitID: fixture.commitID, ExpectedCleanupDueAtMS: fixture.cleanupDueAtMS,
		})
	case OperationMaintainRateScopes:
		return NewMaintainRateScopesWireRequest(gate, fixture.rankOffset)
	default:
		return OperationWireRequest{}, ErrUnknownOperation
	}
}

func newWireOracleFixture(t *testing.T) *wireOracleFixture {
	t.Helper()
	runID := RunID(strings.Repeat("1", 32))
	otherRunID := RunID(strings.Repeat("8", 32))
	bootEvidenceSHA256 := wireOracleDigestCanary("boot-evidence")
	plannedShutdownEvidenceSHA256 := wireOracleDigestCanary("planned-shutdown-evidence")
	installProcessStopEvidenceSHA256 := wireOracleDigestCanary("install-process-stop-evidence")
	shutdownProcessStopEvidenceSHA256 := wireOracleDigestCanary("shutdown-process-stop-evidence")
	sourceSHA256 := wireOracleDigestCanary("crawl-source")
	authorizationSHA256 := wireOracleDigestCanary("authorization")
	authorizationScopeSHA256 := wireOracleDigestCanary("authorization-scope")
	canonicalizationSHA256 := wireOracleDigestCanary("canonicalization")
	crawlPolicySHA256 := wireOracleDigestCanary("crawl-policy")
	renderPolicySHA256 := plainSHA256(testDenyAllRenderPolicyArtifact())
	archiveSHA256 := wireOracleDigestCanary("archive")
	purgeEvidenceSHA256 := wireOracleDigestCanary("purge-evidence")
	rateScopeID := RateScopeID(strings.Repeat("2", 32))
	target := wireOracleTarget(t, "https://example.com/")
	decision, err := NewPolicyDecision(PolicyDecisionInput{
		RequestKind: RequestDocument, Target: target, Depth: 1, GroupID: GroupID("default"), RateScopeID: rateScopeID,
		GroupConcurrency: 1, GroupIntervalMS: 0, OriginConcurrency: 1, OriginIntervalMS: 0,
	})
	if err != nil {
		t.Fatalf("construct source policy decision: %v", err)
	}
	score, err := ParseScoreText("1")
	if err != nil {
		t.Fatalf("construct source score: %v", err)
	}
	job := SourceJob{
		JobID: target.URLID, CanonicalURL: target.CanonicalURL, ScoreText: score, Depth: 1,
		GroupID: GroupID("default"), RateScopeID: rateScopeID, Decision: decision,
	}
	policyGroup := PolicyGroup{
		GroupID: GroupID("default"), RateScopeID: rateScopeID, GroupScopeID: decision.GroupScopeID,
		RequestStartLimit: MaxRequestStartsPerGroup, Concurrency: 1, IntervalMS: 0,
	}
	policyGroupMap, err := DerivePolicyGroupMapDigest([]PolicyGroup{policyGroup})
	if err != nil {
		t.Fatalf("construct policy-group map digest: %v", err)
	}
	lease := LeaseIdentity{
		RunID: runID, JobID: job.JobID, OwnerID: OwnerID(strings.Repeat("3", 32)),
		Fence: Fence(2), Token: LeaseToken(strings.Repeat("4", 64)),
	}
	intent := ReservationIntent{
		Lease: lease, RequestOrdinal: 1, Target: target, CrawlPolicyDigest: crawlPolicySHA256, Decision: decision,
	}
	runPolicy := newAuthenticatedTestRunPolicyAuthority(t, runID, crawlPolicySHA256, renderPolicySHA256, []PolicyGroup{policyGroup})
	reservationID, err := DeriveReservationID(runPolicy, intent)
	if err != nil {
		t.Fatalf("construct reservation identity: %v", err)
	}
	event := newTestSuccessfulStartEventWithAttempts(t, runPolicy, job, lease, crawlPolicySHA256, RequestDocument, target, 3, 1000, 3, 3, 3, 2)
	transcript, err := NewDocumentTranscript(runPolicy, job, event)
	if err != nil {
		t.Fatal(err)
	}
	witness, err := newTestTransportAuthority().parseFinalDocumentWitness(lease, []string{
		"1000", "2", string(target.URLID), target.CanonicalURL, string(decision.TargetDigest), "3", "1000",
		"2", "leased", string(lease.OwnerID), string(lease.Token), "2", "",
	})
	if err != nil {
		t.Fatal(err)
	}
	renderPolicy, err := NewRenderPolicyAuthorization(runPolicy, testDenyAllRenderPolicyArtifact())
	if err != nil {
		t.Fatal(err)
	}
	outputContext, err := NewOutputContext(runPolicy, job, transcript, witness, renderPolicy)
	if err != nil {
		t.Fatal(err)
	}
	outlink := wireOracleTarget(t, "https://example.com/outlink")
	imageURL := wireOracleTarget(t, "https://example.com/image.png").CanonicalURL
	discoveryTarget := wireOracleTarget(t, "https://example.com/discovered")
	discoveryDecision, err := NewPolicyDecision(PolicyDecisionInput{
		RequestKind: RequestDocument, Target: discoveryTarget, Depth: 2, GroupID: job.GroupID, RateScopeID: job.RateScopeID,
		GroupConcurrency: 1, GroupIntervalMS: 0, OriginConcurrency: 1, OriginIntervalMS: 0,
	})
	if err != nil {
		t.Fatalf("construct discovery policy decision: %v", err)
	}
	discoveryDecisionDigest, err := DerivePolicyDecisionDigest(discoveryDecision)
	if err != nil {
		t.Fatalf("construct discovery policy digest: %v", err)
	}
	discoveryScore, err := ParseScoreText("2")
	if err != nil {
		t.Fatal(err)
	}
	output := CrawlOutput{
		Page:     OutputPage{NormalizedURL: target.CanonicalURL, HTML: []byte("<html><body>oracle</body></html>"), ContentType: "text/html", StatusCode: 200},
		Outlinks: []string{outlink.CanonicalURL},
		Images:   []OutputImage{{NormalizedSourceURL: imageURL, Alt: "deterministic oracle image"}},
		Discoveries: []OutputDiscovery{{JobID: discoveryTarget.URLID, CanonicalURL: discoveryTarget.CanonicalURL, Depth: 2,
			ScoreText: discoveryScore, GroupID: job.GroupID, RateScopeID: job.RateScopeID, Decision: discoveryDecision}},
	}
	// Literal semantic records and independent framing establish the expected
	// BEGIN digest. The production BEGIN constructor must derive it from output.
	semanticSections := make([][]Record, 5)
	semanticSections[0] = []Record{{wireOracleField("normalized_url", target.CanonicalURL), wireOracleField("html", "<html><body>oracle</body></html>"),
		wireOracleField("original_html", ""), wireOracleField("content_type", "text/html"), wireOracleField("status_code", "200"),
		wireOracleField("last_crawled", "Thu, 01 Jan 1970 00:00:01 UTC"), wireOracleField("rendered", "false"),
		wireOracleField("render_policy_rule", ""), wireOracleField("render_policy_sha256", "")}}
	semanticSections[1] = []Record{{wireOracleField("target_url", outlink.CanonicalURL)}}
	semanticSections[2] = []Record{{wireOracleField("normalized_source_url", imageURL), wireOracleField("alt", "deterministic oracle image")}}
	semanticSections[3] = []Record{{wireOracleField("job_id", string(discoveryTarget.URLID)), wireOracleField("canonical_url", discoveryTarget.CanonicalURL),
		wireOracleField("depth", "2"), wireOracleField("score_text", "2"), wireOracleField("group_id", string(job.GroupID)),
		wireOracleField("rate_scope_id", string(job.RateScopeID)), wireOracleField("policy_decision_sha256", string(discoveryDecisionDigest))}}
	semanticSections[4] = []Record{{wireOracleField("url_id", string(target.URLID)), wireOracleField("canonical_url", target.CanonicalURL), wireOracleField("depth", "1")}}
	outputDigest := wireOracleOutputDigest(semanticSections)
	publicationID := wireOracleFramedDigest("mifolyo:page-publication:v2", string(runID), string(job.JobID), "2", string(outputDigest))
	commitID := wireOracleFramedDigest("mifolyo:crawl-commit:v2", string(runID), string(job.JobID), "2", string(lease.Token), string(publicationID), "2", "3")
	commitIdentity := CommitIdentity{
		RunID: runID, JobID: job.JobID, OwnerID: lease.OwnerID, Fence: lease.Fence,
		Token: lease.Token, PublicationID: publicationID, RequestStartsBaseline: 2, RequestStartsGeneration: 3,
	}

	pageFields := wireOracleChunk(t, commitID, ChunkPageFields, 0, []Record{{
		wireOracleField("normalized_url", target.CanonicalURL),
		wireOracleField("content_type", "text/html"),
		wireOracleField("status_code", "200"),
		wireOracleField("last_crawled", "Thu, 01 Jan 1970 00:00:01 UTC"),
		wireOracleField("rendered", "false"),
		wireOracleField("render_policy_rule", ""),
		wireOracleField("render_policy_sha256", ""),
		wireOracleField("publication_id", string(publicationID)),
	}})
	pageBlob := wireOracleChunk(t, commitID, ChunkHTML, 0, []Record{{
		wireOracleField("field_name", string(ChunkHTML)),
		{Name: "field_bytes", Value: []byte("<html><body>oracle</body></html>")},
	}})
	outlinks := wireOracleChunk(t, commitID, ChunkOutlinks, 0, []Record{{
		wireOracleField("target_url", outlink.CanonicalURL),
	}})

	discoveries := wireOracleChunk(t, commitID, ChunkDiscoveries, 0, []Record{{
		wireOracleField("job_id", string(discoveryTarget.URLID)),
		wireOracleField("canonical_url", discoveryTarget.CanonicalURL),
		wireOracleField("depth", "2"),
		wireOracleField("score_text", "2"),
		wireOracleField("group_id", string(job.GroupID)),
		wireOracleField("rate_scope_id", string(job.RateScopeID)),
		wireOracleField("group_scope_id", string(discoveryDecision.GroupScopeID)),
		wireOracleField("initial_origin_scope_id", string(discoveryDecision.OriginScopeID)),
		wireOracleField("policy_decision_sha256", string(discoveryDecisionDigest)),
	}})
	aliases := wireOracleChunk(t, commitID, ChunkAliases, 0, []Record{{
		wireOracleField("url_id", string(target.URLID)),
		wireOracleField("canonical_url", target.CanonicalURL),
		wireOracleField("depth", "1"),
	}})
	images := wireOracleChunk(t, commitID, ChunkImages, 0, []Record{{
		wireOracleField("normalized_source_url", imageURL),
		wireOracleField("alt", "deterministic oracle image"),
	}})
	imageKey, err := ImageDataKey(publicationID, target.CanonicalURL, imageURL)
	if err != nil {
		t.Fatalf("construct image-manifest key: %v", err)
	}
	imageKeys, err := json.Marshal([]string{imageKey})
	if err != nil {
		t.Fatalf("construct image-manifest JSON: %v", err)
	}
	manifest := wireOracleChunk(t, commitID, ChunkImageManifest, 0, []Record{{
		wireOracleField("contract_version", "1"),
		wireOracleField("publication_id", string(publicationID)),
		wireOracleField("normalized_url", target.CanonicalURL),
		wireOracleField("image_count", "1"),
		{Name: "image_keys", Value: imageKeys},
	}})
	manifestChunkDigest, err := DeriveChunkDigest(manifest)
	if err != nil {
		t.Fatalf("construct manifest chunk digest: %v", err)
	}

	artifacts, retireInput := newWireOracleArtifacts(t, runID)
	fixture := &wireOracleFixture{
		runID: runID, otherRunID: otherRunID,
		bootEvidenceSHA256: bootEvidenceSHA256, plannedShutdownEvidenceSHA256: plannedShutdownEvidenceSHA256,
		installProcessStopEvidenceSHA256:  installProcessStopEvidenceSHA256,
		shutdownProcessStopEvidenceSHA256: shutdownProcessStopEvidenceSHA256,
		sourceSHA256:                      sourceSHA256, authorizationSHA256: authorizationSHA256,
		authorizationScopeSHA256: authorizationScopeSHA256, canonicalizationSHA256: canonicalizationSHA256,
		crawlPolicySHA256: crawlPolicySHA256, renderPolicySHA256: renderPolicySHA256,
		archiveSHA256: archiveSHA256, purgeEvidenceSHA256: purgeEvidenceSHA256,
		policyGroup: policyGroup, policyGroupMap: policyGroupMap, runPolicy: runPolicy,
		job: job, discoveryDecision: discoveryDecision, lease: lease, intent: intent, reservationID: reservationID,
		outputDigest: outputDigest, publicationID: publicationID, commitIdentity: commitIdentity, commitID: commitID,
		outputContext:       outputContext,
		output:              output,
		manifestChunkDigest: manifestChunkDigest, rankOffset: 7, cleanupDueAtMS: 2_000,
		retireInput: retireInput, artifacts: artifacts,
		chunks: map[OperationName]StageChunk{
			OperationStagePageFields:       pageFields,
			OperationStagePageBlob:         pageBlob,
			OperationStageOutlinksBatch:    outlinks,
			OperationStageDiscoveriesBatch: discoveries,
			OperationStageAliasesBatch:     aliases,
			OperationStageImagesBatch:      images,
			OperationStageImageManifest:    manifest,
		},
	}
	for operation, chunk := range fixture.chunks {
		bound, err := newBoundStageChunk(commitIdentity, outputContext, chunk.kind, chunk.ordinal, chunk.records)
		if err != nil {
			t.Fatal(err)
		}
		fixture.chunks[operation] = bound
	}
	fixture.semanticCanaries = newWireOracleSemanticCanaries(t, fixture)
	return fixture
}

func newWireOracleArtifacts(t *testing.T, candidateRunID RunID) (wireOracleArtifacts, RetireLegacyKeysWireInput) {
	t.Helper()
	return newWireOracleArtifactsForContract(t, candidateRunID, wireOracleDigestCanary("contract"))
}

// Test-only contract seam: regenerate the entire acyclic artifact chain, never
// patch a digest into already constructed guard/compatibility/freeze records.
func newWireOracleArtifactsForContract(t *testing.T, candidateRunID RunID, contractSHA256 Digest) (wireOracleArtifacts, RetireLegacyKeysWireInput) {
	t.Helper()
	redisConfigSHA256 := wireOracleDigestCanary("redis-config")
	core, err := NewGuardCore(GuardCoreInput{
		ContractSHA256: contractSHA256, RedisVersion: "7.2.5", RedisConfigSHA256: redisConfigSHA256,
		MaximumShapeSHA256:     wireOracleDigestCanary("maximum-shape"),
		MemoryFixtureSHA256:    wireOracleDigestCanary("memory-fixture"),
		LuaBenchmarkSHA256:     wireOracleDigestCanary("lua-benchmark"),
		AOFCrashEvidenceSHA256: wireOracleDigestCanary("aof-crash-evidence"),
		CutoverMode:            CutoverV1Migration, CandidateRunID: candidateRunID,
	})
	if err != nil {
		t.Fatalf("construct oracle guard core: %v", err)
	}
	coreDigest, err := core.SHA256()
	if err != nil {
		t.Fatalf("digest oracle guard core: %v", err)
	}
	artifact, err := NewCompatibilityArtifact(CompatibilityArtifactInput{
		RedisConfigSHA256: redisConfigSHA256, CommitGuardSHA256: coreDigest,
		SpiderImage:             wireOracleImageDigestCanary("spider-image"),
		SeedImporterImage:       wireOracleImageDigestCanary("seed-importer-image"),
		CrawlAdminImage:         wireOracleImageDigestCanary("crawl-admin-image"),
		IndexerImage:            wireOracleImageDigestCanary("indexer-image"),
		ImageIndexerImage:       wireOracleImageDigestCanary("image-indexer-image"),
		BacklinksProcessorImage: wireOracleImageDigestCanary("backlinks-processor-image"),
		MonitoringImage:         wireOracleImageDigestCanary("monitoring-image"),
		RenderWorkerImage:       "disabled",
	})
	if err != nil {
		t.Fatalf("construct oracle compatibility artifact: %v", err)
	}
	marker, err := NewCompatibilityMarker(artifact)
	if err != nil {
		t.Fatalf("construct oracle compatibility marker: %v", err)
	}
	manifestDigest, err := marker.ManifestSHA256()
	if err != nil {
		t.Fatalf("digest oracle compatibility marker: %v", err)
	}
	guard, err := NewStoredCommitGuard(core, manifestDigest, 3)
	if err != nil {
		t.Fatalf("construct oracle stored guard: %v", err)
	}
	freezeNonce := strings.Repeat("c", 32)
	freeze, err := NewAdminFreezeRecord(AdminFreezeRecordInput{
		FreezeNonce: freezeNonce, ProcessStopEvidenceSHA256: wireOracleDigestCanary("freeze-process-stop-evidence"),
		CandidateManifestSHA256: manifestDigest, CandidateContractSHA256: contractSHA256, CreatedAtMS: 4,
	})
	if err != nil {
		t.Fatalf("construct oracle admin freeze: %v", err)
	}
	legacyInput := LegacyRetirementRecordInput{
		FreezeNonce: freezeNonce, BackupSHA256: wireOracleDigestCanary("legacy-backup"),
		V1Count: 1, V1URLFieldCount: 1, V1DepthFieldCount: 1,
		V1SourceSHA256:         wireOracleDigestCanary("legacy-v1-source"),
		V1QueueEvidenceSHA256:  wireOracleDigestCanary("legacy-v1-queue-evidence"),
		V1URLsEvidenceSHA256:   wireOracleDigestCanary("legacy-v1-urls-evidence"),
		V1DepthsEvidenceSHA256: wireOracleDigestCanary("legacy-v1-depths-evidence"),
		SpiderQueueType:        LegacyTypeNone, SpiderQueueCount: 0,
		SpiderQueueEvidenceSHA256: wireOracleDigestCanary("legacy-spider-queue-evidence"),
		SignalQueueType:           LegacyTypeNone, SignalQueueCount: 0,
		SignalQueueEvidenceSHA256: wireOracleDigestCanary("legacy-signal-queue-evidence"),
		DeletedBitmap:             "11100", RetiredAtMS: 5,
	}
	legacy, err := NewLegacyRetirementRecord(legacyInput)
	if err != nil {
		t.Fatalf("construct oracle legacy retirement record: %v", err)
	}
	retireInput := RetireLegacyKeysWireInput{
		FreezeNonce: freezeNonce, BackupSHA256: legacyInput.BackupSHA256,
		V1Count: 1, V1URLFieldCount: 1, V1DepthFieldCount: 1,
		V1SourceSHA256:         legacyInput.V1SourceSHA256,
		V1QueueEvidenceSHA256:  legacyInput.V1QueueEvidenceSHA256,
		V1URLsEvidenceSHA256:   legacyInput.V1URLsEvidenceSHA256,
		V1DepthsEvidenceSHA256: legacyInput.V1DepthsEvidenceSHA256,
		SpiderQueueType:        LegacyTypeNone, SpiderQueueCount: 0,
		SpiderQueueEvidenceSHA256: legacyInput.SpiderQueueEvidenceSHA256,
		SignalQueueType:           LegacyTypeNone, SignalQueueCount: 0,
		SignalQueueEvidenceSHA256: legacyInput.SignalQueueEvidenceSHA256,
	}
	return wireOracleArtifacts{
		bootEpoch: strings.Repeat("d", 32), contract: contractSHA256, marker: marker,
		guard: guard, legacy: legacy, freeze: freeze, core: core,
	}, retireInput
}

func wireOracleDigestCanary(label string) Digest {
	sum := sha256.Sum256([]byte("mifolyo:wire-oracle-canary:" + label))
	return Digest(hex.EncodeToString(sum[:]))
}

func wireOracleImageDigestCanary(label string) ImageDigest {
	return ImageDigest("sha256:" + wireOracleDigestCanary(label))
}

func newWireOracleSemanticCanaries(t *testing.T, fixture *wireOracleFixture) map[OperationName]map[string]string {
	t.Helper()
	compatibility := fixture.artifacts.marker.artifact.input
	core := fixture.artifacts.core.input
	coreDigest, err := fixture.artifacts.core.SHA256()
	if err != nil {
		t.Fatalf("derive oracle guard-core canary: %v", err)
	}
	manifestDigest, err := fixture.artifacts.marker.ManifestSHA256()
	if err != nil {
		t.Fatalf("derive oracle manifest canary: %v", err)
	}
	jobDecisionDigest, err := DerivePolicyDecisionDigest(fixture.job.Decision)
	if err != nil {
		t.Fatalf("derive oracle job-decision canary: %v", err)
	}
	reservationDecisionDigest, err := DerivePolicyDecisionDigest(fixture.intent.Decision)
	if err != nil {
		t.Fatalf("derive oracle reservation-decision canary: %v", err)
	}
	discoveryDecisionDigest, err := DerivePolicyDecisionDigest(fixture.discoveryDecision)
	if err != nil {
		t.Fatalf("derive oracle discovery-decision canary: %v", err)
	}
	mustTransitionID := func(label string, derive func() (Digest, error)) string {
		t.Helper()
		digest, err := derive()
		if err != nil {
			t.Fatalf("derive oracle %s transition canary: %v", label, err)
		}
		return string(digest)
	}
	rejectTransitionID := mustTransitionID("reject-ready", func() (Digest, error) {
		return DeriveRejectReadyTransitionID(fixture.runPolicy, RejectReadyTransitionInput{
			RunID: fixture.runID, Job: fixture.job, Reason: ReasonPolicyDenied,
		})
	})
	claimTransitionID := mustTransitionID("try-claim", func() (Digest, error) {
		return DeriveTryClaimTransitionID(fixture.runPolicy, TryClaimTransitionInput{
			Job: fixture.job, Lease: fixture.lease, ExpectedPriorFence: 1, InitialIntent: fixture.intent,
		})
	})
	releaseTransitionID := mustTransitionID("release-before-io", func() (Digest, error) {
		return DeriveReleaseBeforeIOTransitionID(ReleaseBeforeIOTransitionInput{Lease: fixture.lease})
	})
	retryTransitionID := mustTransitionID("retry", func() (Digest, error) {
		return DeriveRetryTransitionID(RetryTransitionInput{Lease: fixture.lease, Reason: ReasonRequestTimeout})
	})
	deadTransitionID := mustTransitionID("dead", func() (Digest, error) {
		return DeriveDeadTransitionID(DeadTransitionInput{Lease: fixture.lease, Reason: ReasonHTTP4xx})
	})
	cancelJobTransitionID := mustTransitionID("cancel-job", func() (Digest, error) {
		return DeriveCancelJobTransitionID(CancelJobTransitionInput{Lease: fixture.lease, Reason: ReasonOperatorCancelled})
	})
	completeTransitionID := mustTransitionID("complete-no-output", func() (Digest, error) {
		return DeriveCompleteNoOutputTransitionID(CompleteNoOutputTransitionInput{Lease: fixture.lease, Reason: ReasonAlreadyVisited})
	})
	abortTransitionID := mustTransitionID("abort-stage", func() (Digest, error) {
		return DeriveAbortStageTransitionID(AbortStageTransitionInput{Lease: fixture.lease, CommitID: fixture.commitID})
	})
	claimCanaries := wireOracleReservationDigestCanaries(fixture, jobDecisionDigest, reservationDecisionDigest)
	claimCanaries["transition_id"] = claimTransitionID

	canaries := map[OperationName]map[string]string{
		OperationApproveBoot: {
			"evidence_sha256": string(fixture.bootEvidenceSHA256),
			"planned_shutdown_evidence_sha256_or_empty": string(fixture.plannedShutdownEvidenceSHA256),
		},
		OperationInstallCandidateMarkers: {
			"process_stop_evidence_sha256": string(fixture.installProcessStopEvidenceSHA256),
			"contract_sha256":              string(fixture.artifacts.contract),
			"manifest_sha256":              string(manifestDigest),
			"redis_config_sha256":          string(compatibility.RedisConfigSHA256),
			"commit_guard_sha256":          string(compatibility.CommitGuardSHA256),
			"spider_image":                 string(compatibility.SpiderImage),
			"seed_importer_image":          string(compatibility.SeedImporterImage),
			"crawl_admin_image":            string(compatibility.CrawlAdminImage),
			"indexer_image":                string(compatibility.IndexerImage),
			"image_indexer_image":          string(compatibility.ImageIndexerImage),
			"backlinks_processor_image":    string(compatibility.BacklinksProcessorImage),
			"monitoring_image":             string(compatibility.MonitoringImage),
			"render_worker_image":          compatibility.RenderWorkerImage,
		},
		OperationRetireLegacyKeys: {
			"backup_sha256":                string(fixture.retireInput.BackupSHA256),
			"v1_source_sha256":             string(fixture.retireInput.V1SourceSHA256),
			"v1_queue_evidence_sha256":     string(fixture.retireInput.V1QueueEvidenceSHA256),
			"v1_urls_evidence_sha256":      string(fixture.retireInput.V1URLsEvidenceSHA256),
			"v1_depths_evidence_sha256":    string(fixture.retireInput.V1DepthsEvidenceSHA256),
			"spider_queue_evidence_sha256": string(fixture.retireInput.SpiderQueueEvidenceSHA256),
			"signal_queue_evidence_sha256": string(fixture.retireInput.SignalQueueEvidenceSHA256),
			"confirmation_text": strings.Join([]string{
				fixture.retireInput.FreezeNonce,
				string(fixture.retireInput.BackupSHA256),
				canonicalDecimal(fixture.retireInput.V1Count),
				string(fixture.retireInput.V1SourceSHA256),
				string(fixture.retireInput.V1QueueEvidenceSHA256),
				string(fixture.retireInput.V1URLsEvidenceSHA256),
				string(fixture.retireInput.V1DepthsEvidenceSHA256),
				string(fixture.retireInput.SpiderQueueEvidenceSHA256),
				string(fixture.retireInput.SignalQueueEvidenceSHA256),
			}, ":"),
		},
		OperationPromoteCandidateContracts: {
			"commit_guard_sha256":       string(coreDigest),
			"contract_sha256":           string(core.ContractSHA256),
			"redis_config_sha256":       string(core.RedisConfigSHA256),
			"maximum_shape_sha256":      string(core.MaximumShapeSHA256),
			"memory_fixture_sha256":     string(core.MemoryFixtureSHA256),
			"lua_benchmark_sha256":      string(core.LuaBenchmarkSHA256),
			"aof_crash_evidence_sha256": string(core.AOFCrashEvidenceSHA256),
		},
		OperationMarkPlannedShutdown: {
			"process_stop_evidence_sha256": string(fixture.shutdownProcessStopEvidenceSHA256),
		},
		OperationCreateRun: {
			"source_sha256":              string(fixture.sourceSHA256),
			"authorization_sha256":       string(fixture.authorizationSHA256),
			"authorization_scope_sha256": string(fixture.authorizationScopeSHA256),
			"canonicalization_sha256":    string(fixture.canonicalizationSHA256),
			"crawl_policy_sha256":        string(fixture.crawlPolicySHA256),
			"render_policy_sha256":       string(fixture.renderPolicySHA256),
			"policy_group_map_sha256":    string(fixture.policyGroupMap),
			"group_scope_id":             string(fixture.policyGroup.GroupScopeID),
		},
		OperationEnqueueBatch: {
			"group_scope_id":          string(fixture.job.Decision.GroupScopeID),
			"initial_origin_scope_id": string(fixture.job.Decision.OriginScopeID),
			"policy_decision_sha256":  string(jobDecisionDigest),
		},
		OperationAuditRunBatch: {
			"group_scope_id":          string(fixture.job.Decision.GroupScopeID),
			"initial_origin_scope_id": string(fixture.job.Decision.OriginScopeID),
			"policy_decision_sha256":  string(jobDecisionDigest),
		},
		OperationSealRun: {
			"source_sha256": string(fixture.sourceSHA256),
		},
		OperationActivateRun: {
			"confirmation_text": strings.Join([]string{
				string(fixture.runID), string(fixture.sourceSHA256), string(fixture.authorizationSHA256), string(fixture.crawlPolicySHA256),
			}, ":"),
			"authorization_sha256":    string(fixture.authorizationSHA256),
			"crawl_policy_sha256":     string(fixture.crawlPolicySHA256),
			"render_policy_sha256":    string(fixture.renderPolicySHA256),
			"canonicalization_sha256": string(fixture.canonicalizationSHA256),
		},
		OperationRejectReady: {
			"group_scope_id":          string(fixture.job.Decision.GroupScopeID),
			"initial_origin_scope_id": string(fixture.job.Decision.OriginScopeID),
			"policy_decision_sha256":  string(jobDecisionDigest),
			"transition_id":           rejectTransitionID,
		},
		OperationTryClaim:       claimCanaries,
		OperationReserveRequest: wireOracleReservationDigestCanaries(fixture, "", reservationDecisionDigest),
		OperationReleaseBeforeIO: {
			"transition_id": releaseTransitionID,
		},
		OperationRetry: {
			"transition_id": retryTransitionID,
		},
		OperationDead: {
			"transition_id": deadTransitionID,
		},
		OperationCancelJob: {
			"transition_id": cancelJobTransitionID,
		},
		OperationCompleteNoOutput: {
			"transition_id": completeTransitionID,
		},
		OperationBeginStage: {
			"commit_id":                 string(fixture.commitID),
			"publication_id":            string(fixture.publicationID),
			"output_digest":             string(fixture.outputDigest),
			"request_starts_baseline":   "2",
			"request_starts_generation": "3",
		},
		OperationStagePageFields: {
			"render_policy_sha256": "",
			"publication_id":       string(fixture.publicationID),
		},
		OperationStageDiscoveriesBatch: {
			"group_scope_id":          string(fixture.discoveryDecision.GroupScopeID),
			"initial_origin_scope_id": string(fixture.discoveryDecision.OriginScopeID),
			"policy_decision_sha256":  string(discoveryDecisionDigest),
		},
		OperationStageImageManifest: {
			"publication_id": string(fixture.publicationID),
		},
		OperationSealStage: {
			"commit_id":                      string(fixture.commitID),
			"verified_output_digest":         string(fixture.outputDigest),
			"verified_manifest_chunk_digest": string(fixture.manifestChunkDigest),
		},
		OperationAbortStage: {
			"commit_id":     string(fixture.commitID),
			"transition_id": abortTransitionID,
		},
		OperationCommit: {
			"commit_id": string(fixture.commitID),
		},
		OperationArchiveRun: {
			"archive_sha256":    string(fixture.archiveSHA256),
			"confirmation_text": string(fixture.runID) + ":" + string(fixture.archiveSHA256),
		},
		OperationPurgeRunBatch: {
			"evidence_sha256": string(fixture.purgeEvidenceSHA256),
		},
		OperationCleanStage: {
			"expected_commit_id": string(fixture.commitID),
		},
	}

	for operation, chunk := range fixture.chunks {
		chunkDigest, err := DeriveChunkDigest(chunk)
		if err != nil {
			t.Fatalf("derive %s chunk canary: %v", operation, err)
		}
		if canaries[operation] == nil {
			canaries[operation] = make(map[string]string)
		}
		canaries[operation]["commit_id"] = string(fixture.commitID)
		canaries[operation]["chunk_digest"] = string(chunkDigest)
	}
	return canaries
}

func wireOracleReservationDigestCanaries(
	fixture *wireOracleFixture,
	jobDecisionDigest Digest,
	reservationDecisionDigest Digest,
) map[string]string {
	values := map[string]string{
		"target_digest":          string(fixture.intent.Decision.TargetDigest),
		"crawl_policy_sha256":    string(fixture.crawlPolicySHA256),
		"policy_decision_sha256": string(reservationDecisionDigest),
		"global_scope_id":        string(fixture.intent.Decision.GlobalScopeID),
		"group_scope_id":         string(fixture.intent.Decision.GroupScopeID),
		"origin_scope_id":        string(fixture.intent.Decision.OriginScopeID),
	}
	if jobDecisionDigest != "" {
		values["job_group_scope_id"] = string(fixture.job.Decision.GroupScopeID)
		values["job_initial_origin_scope_id"] = string(fixture.job.Decision.OriginScopeID)
		values["job_policy_decision_sha256"] = string(jobDecisionDigest)
	}
	return values
}

func wireOracleTarget(t *testing.T, rawURL string) RequestTarget {
	t.Helper()
	identity, err := requireCanonicalURL(rawURL)
	if err != nil {
		t.Fatalf("construct canonical target %q: %v", rawURL, err)
	}
	return RequestTarget{URLID: JobID(identity.URLID), CanonicalURL: identity.CanonicalURL}
}

func wireOracleField(name, value string) Field {
	return Field{Name: name, Value: []byte(value)}
}

// Independent output/identity oracle. None of these helpers call the production
// framing, section, output-digest, publication, or commit implementations.
func wireOracleU64(value uint64) []byte {
	encoded := make([]byte, 8)
	for index := range encoded {
		encoded[index] = byte(value >> uint(56-index*8))
	}
	return encoded
}

func wireOracleFrame(value []byte) []byte {
	return append(wireOracleU64(uint64(len(value))), value...)
}

func wireOracleFramedDigest(values ...string) Digest {
	var encoded []byte
	for _, value := range values {
		encoded = append(encoded, wireOracleFrame([]byte(value))...)
	}
	sum := sha256.Sum256(encoded)
	return Digest(hex.EncodeToString(sum[:]))
}

func wireOracleOutputDigest(sections [][]Record) Digest {
	encoded := wireOracleFrame([]byte("mifolyo:crawl-output:v2"))
	for index, label := range []string{"page", "outlinks", "images", "discoveries", "aliases"} {
		encoded = append(encoded, wireOracleFrame([]byte(label))...)
		encoded = append(encoded, wireOracleU64(uint64(len(sections[index])))...)
		for _, record := range sections[index] {
			encodedRecord := wireOracleU64(uint64(len(record)))
			for _, field := range record {
				encodedRecord = append(encodedRecord, wireOracleFrame([]byte(field.Name))...)
				encodedRecord = append(encodedRecord, wireOracleFrame(field.Value)...)
			}
			encoded = append(encoded, wireOracleFrame(encodedRecord)...)
		}
	}
	sum := sha256.Sum256(encoded)
	return Digest(hex.EncodeToString(sum[:]))
}

func wireOracleChunk(t *testing.T, commitID Digest, kind ChunkKind, ordinal uint64, records []Record) StageChunk {
	t.Helper()
	chunk, err := newValidatedStageChunk(commitID, kind, ordinal, records)
	if err != nil {
		t.Fatalf("construct %s oracle chunk: %v", kind, err)
	}
	return chunk
}

func wireOracleExpectedKeys(fixture *wireOracleFixture, expectation wireOracleOperationExpectation) []string {
	if expectation.keyPlan == wireOracleKeysApproveBoot {
		return []string{"mifolyo:crawl:v2:durability"}
	}
	keys := wireOracleAuthorityKeys()
	switch expectation.keyPlan {
	case wireOracleKeysAuthority:
	case wireOracleKeysRetire:
		keys = append(keys,
			"mifolyo:crawl:v2:runs",
			"mifolyo:crawl:v2:active_runs",
			"mifolyo:crawl:v2:unarchived_runs",
		)
		keys = append(keys, wireOracleLegacyKeys()...)
	case wireOracleKeysPromote:
		keys = append(keys,
			"mifolyo:crawl:v2:runs",
			"mifolyo:crawl:v2:active_runs",
			"mifolyo:crawl:v2:unarchived_runs",
			"mifolyo:crawl:v2:first_request_start",
			"mifolyo:crawl:v2:active_leases",
			"mifolyo:crawl:v2:stage_expiry",
			"mifolyo:crawl:v2:stage_slots",
			"mifolyo:crawl:v2:rate_scopes",
		)
		keys = append(keys, wireOracleLegacyKeys()...)
		keys = append(keys, wireOracleDownstreamKeys()...)
		keys = append(keys, wireOracleRunKeys(fixture.runID)...)
	case wireOracleKeysMarkShutdown:
		keys = append(keys,
			"mifolyo:crawl:v2:active_runs",
			"mifolyo:crawl:v2:active_leases",
			"mifolyo:crawl:v2:stage_slots",
			"mifolyo:crawl:v2:stage_expiry",
			"mifolyo:crawl:v2:rate_scopes",
		)
		keys = append(keys, wireOracleRateScopeKeys(fixture.intent.Decision.GlobalScopeID)...)
		keys = append(keys, "pages_queue:indexer_owner", "image_indexer_queue:owner")
		for _, runID := range []RunID{fixture.runID, fixture.otherRunID} {
			base := "mifolyo:crawl:v2:run:" + string(runID)
			keys = append(keys, base, base+":leased")
		}
	case wireOracleKeysCreateRun:
		keys = append(keys,
			"mifolyo:crawl:v2:runs",
			"mifolyo:crawl:v2:active_runs",
			"mifolyo:crawl:v2:unarchived_runs",
		)
		keys = append(keys, wireOracleRunKeys(fixture.runID)...)
	case wireOracleKeysRunRecords:
		keys = append(keys,
			"mifolyo:crawl:v2:runs",
			"mifolyo:crawl:v2:active_runs",
			"mifolyo:crawl:v2:unarchived_runs",
		)
		keys = append(keys, wireOracleRunKeys(fixture.runID)...)
		keys = append(keys, wireOracleRunJobKey(fixture.runID, fixture.job.JobID))
	case wireOracleKeysRun:
		keys = append(keys,
			"mifolyo:crawl:v2:runs",
			"mifolyo:crawl:v2:active_runs",
			"mifolyo:crawl:v2:unarchived_runs",
		)
		keys = append(keys, wireOracleRunKeys(fixture.runID)...)
		if expectation.operation == OperationActivateRun {
			keys = append(keys, wireOracleLegacyKeys()...)
		}
	case wireOracleKeysJob:
		keys = append(keys, wireOracleRuntimeRunKeys(fixture.runID, fixture.job.JobID)...)
	case wireOracleKeysReservation:
		keys = append(keys, wireOracleRuntimeRunKeys(fixture.runID, fixture.job.JobID)...)
		keys = append(keys, "mifolyo:crawl:v2:reservation:"+string(fixture.reservationID))
		keys = append(keys, wireOracleRateScopeKeys(fixture.intent.Decision.GlobalScopeID)...)
		keys = append(keys, wireOracleRateScopeKeys(fixture.intent.Decision.GroupScopeID)...)
		keys = append(keys, wireOracleRateScopeKeys(fixture.intent.Decision.OriginScopeID)...)
	case wireOracleKeysStage:
		keys = append(keys, wireOracleRuntimeRunKeys(fixture.runID, fixture.job.JobID)...)
		keys = append(keys, wireOracleStageKeys(fixture.commitID)...)
		if expectation.operation == OperationCommit {
			keys = append(keys, "pages_queue")
		}
	case wireOracleKeysRunMaintenance:
		keys = append(keys,
			"mifolyo:crawl:v2:runs",
			"mifolyo:crawl:v2:active_runs",
			"mifolyo:crawl:v2:unarchived_runs",
			"mifolyo:crawl:v2:active_leases",
			"mifolyo:crawl:v2:stage_expiry",
			"mifolyo:crawl:v2:stage_slots",
			"mifolyo:crawl:v2:rate_scopes",
		)
		keys = append(keys, wireOracleRunKeys(fixture.runID)...)
	case wireOracleKeysArchive:
		keys = append(keys,
			"mifolyo:crawl:v2:runs",
			"mifolyo:crawl:v2:active_runs",
			"mifolyo:crawl:v2:unarchived_runs",
			"mifolyo:crawl:v2:active_leases",
			"mifolyo:crawl:v2:stage_expiry",
			"mifolyo:crawl:v2:stage_slots",
		)
		keys = append(keys, wireOracleRunKeys(fixture.runID)...)
		keys = append(keys, wireOracleDownstreamKeys()...)
	case wireOracleKeysPurge:
		keys = append(keys,
			"mifolyo:crawl:v2:runs",
			"mifolyo:crawl:v2:active_runs",
			"mifolyo:crawl:v2:unarchived_runs",
			"mifolyo:crawl:v2:first_request_start",
			"mifolyo:crawl:v2:active_leases",
			"mifolyo:crawl:v2:stage_expiry",
			"mifolyo:crawl:v2:stage_slots",
		)
		keys = append(keys, wireOracleRunKeys(fixture.runID)...)
		keys = append(keys, wireOracleRunJobKey(fixture.runID, fixture.job.JobID))
	case wireOracleKeysCleanStage:
		keys = append(keys, "mifolyo:crawl:v2:stage_expiry", "mifolyo:crawl:v2:stage_slots")
		keys = append(keys, wireOracleStageKeys(fixture.commitID)...)
	case wireOracleKeysRateMaintenance:
		keys = append(keys, "mifolyo:crawl:v2:rate_scopes")
	}
	return keys
}

func wireOracleAuthorityKeys() []string {
	return []string{
		"mifolyo:crawl:v2:durability",
		"mifolyo:contracts:active",
		"mifolyo:crawl:v2:contract",
		"mifolyo:contracts:candidate",
		"mifolyo:crawl:v2:contract:candidate",
		"mifolyo:crawl:v2:commit_guard",
		"mifolyo:crawl:v2:legacy_retirement",
		"mifolyo:crawl:v2:admin_freeze",
	}
}

func wireOracleLegacyKeys() []string {
	return []string{
		"mifolyo:crawl:v1:queue",
		"mifolyo:crawl:v1:urls",
		"mifolyo:crawl:v1:depths",
		"spider_queue",
		"signal_queue",
	}
}

func wireOracleDownstreamKeys() []string {
	return []string{
		"pages_queue",
		"pages_queue:processing",
		"pages_queue:dead",
		"image_indexer_queue",
		"image_indexer_queue:processing",
		"image_indexer_queue:dead",
		"pages_queue:indexer_owner",
		"image_indexer_queue:owner",
	}
}

func wireOracleRuntimeRunKeys(runID RunID, jobID JobID) []string {
	keys := []string{
		"mifolyo:crawl:v2:runs",
		"mifolyo:crawl:v2:active_runs",
		"mifolyo:crawl:v2:unarchived_runs",
		"mifolyo:crawl:v2:first_request_start",
		"mifolyo:crawl:v2:active_leases",
		"mifolyo:crawl:v2:stage_expiry",
		"mifolyo:crawl:v2:stage_slots",
		"mifolyo:crawl:v2:rate_scopes",
	}
	keys = append(keys, wireOracleRunKeys(runID)...)
	return append(keys, wireOracleRunJobKey(runID, jobID))
}

func wireOracleRunKeys(runID RunID) []string {
	base := "mifolyo:crawl:v2:run:" + string(runID)
	suffixes := []string{
		"",
		":jobs",
		":job_order",
		":ready",
		":ready_at",
		":leased",
		":leased_at",
		":delayed",
		":commit_backpressure",
		":completed",
		":dead",
		":cancelled",
		":group_limits",
		":group_rate_scope_ids",
		":group_scope_ids",
		":group_concurrency",
		":group_interval_ms",
		":group_started",
		":group_pending",
		":group_active_started",
		":group_open_jobs",
		":audit_group_counts",
		":retry_reason_counts",
		":recovery_outcome_counts",
		":disposition_reason_counts",
		":visited_depth",
		":visited_urls",
	}
	keys := make([]string, len(suffixes))
	for index, suffix := range suffixes {
		keys[index] = base + suffix
	}
	return keys
}

func wireOracleRunJobKey(runID RunID, jobID JobID) string {
	return "mifolyo:crawl:v2:run:" + string(runID) + ":job:" + string(jobID)
}

func wireOracleStageKeys(commitID Digest) []string {
	base := "mifolyo:crawl:v2:stage:" + string(commitID)
	suffixes := []string{
		":meta",
		":keys",
		":page",
		":outlinks",
		":discoveries",
		":discovery_records",
		":discovery_depths",
		":aliases",
		":image_manifest",
	}
	keys := make([]string, 0, 73)
	for _, suffix := range suffixes {
		keys = append(keys, base+suffix)
	}
	for index := 0; index < 64; index++ {
		keys = append(keys, base+":image:"+strconv.Itoa(index))
	}
	return keys
}

func wireOracleRateScopeKeys(scopeID Digest) []string {
	base := "mifolyo:crawl:v2:rate:" + string(scopeID)
	return []string{base, base + ":active", base + ":pending", base + ":started"}
}
