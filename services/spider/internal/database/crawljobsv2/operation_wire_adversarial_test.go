package crawljobsv2

import (
	"errors"
	"strings"
	"testing"
)

func TestOperationWireSpecificationIsClosedAndComplete(t *testing.T) {
	if len(operationWireOrder) != len(operations) || len(operationWireSpecifications) != len(operations) {
		t.Fatalf("operation wire coverage = order:%d specs:%d operations:%d", len(operationWireOrder), len(operationWireSpecifications), len(operations))
	}
	seen := make(map[OperationName]struct{}, len(operationWireOrder))
	for _, operation := range operationWireOrder {
		if _, duplicate := seen[operation]; duplicate {
			t.Fatalf("duplicate operation %q", operation)
		}
		seen[operation] = struct{}{}
		specification, ok := operationWireSpecifications[operation]
		if !ok || specification.operation != operation || specification.keyPlan == 0 || specification.requestByteLimit == 0 {
			t.Fatalf("incomplete specification for %q", operation)
		}
		expectedLimit, err := evalSHAOperationLimit(operation)
		if err != nil || specification.requestByteLimit != expectedLimit {
			t.Fatalf("request limit for %q = %d, want %d (err=%v)", operation, specification.requestByteLimit, expectedLimit, err)
		}
		allowed, err := AllowedGateModes(operation)
		if err != nil || !equalGateModes(specification.gateModes, allowed) {
			t.Fatalf("gate modes for %q = %v, want %v (err=%v)", operation, specification.gateModes, allowed, err)
		}
		fieldNames := make(map[string]struct{}, len(specification.semanticFields))
		for _, name := range specification.semanticFields {
			if name == "" {
				t.Fatalf("empty semantic field for %q", operation)
			}
			if _, duplicate := fieldNames[name]; duplicate {
				t.Fatalf("duplicate semantic field %q for %q", name, operation)
			}
			fieldNames[name] = struct{}{}
		}
		if specification.tailKind != wireTailNone {
			if _, ok := fieldNames[specification.tailCountField]; !ok || specification.maximumRecords < specification.minimumRecords {
				t.Fatalf("invalid tail specification for %q", operation)
			}
		}
	}
}

// TestOperationWireAllConstructorsRejectZeroSHA256 is a literal ingress
// manifest for the complete constructor family. Operations without a
// digest-bearing operation input are exercised with a zero contract digest in
// their already-constructed opaque gate; the constructor must revalidate that
// gate rather than trusting its private representation.
func TestOperationWireAllConstructorsRejectZeroSHA256(t *testing.T) {
	type zeroMutation func(*wireOracleFixture, *TransportGate)
	zeroDigest := Digest(ZeroSHA256)
	mutations := map[OperationName]zeroMutation{
		OperationApproveBoot: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.bootEvidenceSHA256 = zeroDigest
		},
		OperationInstallCandidateMarkers: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.installProcessStopEvidenceSHA256 = zeroDigest
		},
		OperationRetireLegacyKeys: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.retireInput.BackupSHA256 = zeroDigest
		},
		OperationPromoteCandidateContracts: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.artifacts.core.input.MaximumShapeSHA256 = zeroDigest
		},
		OperationMarkPlannedShutdown: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.shutdownProcessStopEvidenceSHA256 = zeroDigest
		},
		OperationCreateRun: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.sourceSHA256 = zeroDigest
		},
		OperationEnqueueBatch: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.job.Decision.TargetDigest = zeroDigest
		},
		OperationBeginRunAudit: zeroWireOracleGateContract,
		OperationAuditRunBatch: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.job.Decision.GroupScopeID = zeroDigest
		},
		OperationSealRun: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.sourceSHA256 = zeroDigest
		},
		OperationActivateRun: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.authorizationSHA256 = zeroDigest
		},
		OperationRejectReady: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.job.Decision.OriginScopeID = zeroDigest
		},
		OperationTryClaim: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.intent.CrawlPolicyDigest = zeroDigest
		},
		OperationRenewLease: zeroWireOracleGateContract,
		OperationReserveRequest: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.intent.Decision.GlobalScopeID = zeroDigest
		},
		OperationStartRequest: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.intent.CrawlPolicyDigest = zeroDigest
		},
		OperationFinishRequest: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.intent.CrawlPolicyDigest = zeroDigest
		},
		OperationCancelReservation: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.intent.CrawlPolicyDigest = zeroDigest
		},
		OperationReleaseBeforeIO:  zeroWireOracleGateContract,
		OperationRetry:            zeroWireOracleGateContract,
		OperationDead:             zeroWireOracleGateContract,
		OperationCancelJob:        zeroWireOracleGateContract,
		OperationCompleteNoOutput: zeroWireOracleGateContract,
		OperationBeginStage: func(fixture *wireOracleFixture, _ *TransportGate) {
			// BEGIN no longer accepts a raw output digest. Its real discovery
			// output still must reject zero policy digests at the joint boundary.
			fixture.output.Discoveries[0].Decision.TargetDigest = zeroDigest
		},
		OperationStagePageFields:       zeroWireOracleChunkCommit(OperationStagePageFields),
		OperationStagePageBlob:         zeroWireOracleChunkCommit(OperationStagePageBlob),
		OperationStageOutlinksBatch:    zeroWireOracleChunkCommit(OperationStageOutlinksBatch),
		OperationStageDiscoveriesBatch: zeroWireOracleChunkCommit(OperationStageDiscoveriesBatch),
		OperationStageAliasesBatch:     zeroWireOracleChunkCommit(OperationStageAliasesBatch),
		OperationStageImagesBatch:      zeroWireOracleChunkCommit(OperationStageImagesBatch),
		OperationStageImageManifest:    zeroWireOracleChunkCommit(OperationStageImageManifest),
		OperationAbortStage: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.commitID = zeroDigest
		},
		OperationSealStage: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.manifestChunkDigest = zeroDigest
		},
		OperationCommit: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.commitIdentity.PublicationID = zeroDigest
		},
		OperationPromoteDue:     zeroWireOracleGateContract,
		OperationRecoverExpired: zeroWireOracleGateContract,
		OperationCancelRun:      zeroWireOracleGateContract,
		OperationCancelBatch:    zeroWireOracleGateContract,
		OperationFinalizeRun:    zeroWireOracleGateContract,
		OperationArchiveRun: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.archiveSHA256 = zeroDigest
		},
		OperationPurgeRunBatch: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.purgeEvidenceSHA256 = zeroDigest
		},
		OperationCleanStage: func(fixture *wireOracleFixture, _ *TransportGate) {
			fixture.commitID = zeroDigest
		},
		OperationMaintainRateScopes: zeroWireOracleGateContract,
	}

	expectations := wireOracleOperationExpectations()
	if len(mutations) != wireOracleConstructorCount || len(expectations) != wireOracleConstructorCount {
		t.Fatalf("zero manifest = %d, constructor oracle = %d, want %d", len(mutations), len(expectations), wireOracleConstructorCount)
	}
	seen := make(map[OperationName]struct{}, len(expectations))
	for _, expectation := range expectations {
		mutation, covered := mutations[expectation.operation]
		if !covered {
			t.Fatalf("zero manifest omits constructor %s", expectation.operation)
		}
		seen[expectation.operation] = struct{}{}
		for _, variant := range expectation.variants {
			variant := variant
			t.Run(string(expectation.operation)+"/"+wireOracleVariantName(variant), func(t *testing.T) {
				fixture := newWireOracleFixture(t)
				gate := wireOracleGate(t, fixture, expectation.operation, variant)
				mutation(fixture, &gate)
				if _, err := wireOracleConstructRequest(fixture, expectation.operation, variant, gate); err == nil {
					t.Fatal("constructor accepted ZERO_SHA256 at its production ingress")
				}
			})
		}
	}
	for operation := range mutations {
		if _, covered := seen[operation]; !covered {
			t.Fatalf("zero manifest contains unknown or unoracled constructor %s", operation)
		}
	}
}

func zeroWireOracleGateContract(_ *wireOracleFixture, gate *TransportGate) {
	gate.arguments[2] = []byte(ZeroSHA256)
}

func zeroWireOracleChunkCommit(operation OperationName) func(*wireOracleFixture, *TransportGate) {
	return func(fixture *wireOracleFixture, _ *TransportGate) {
		chunk := fixture.chunks[operation]
		chunk.commitID = Digest(ZeroSHA256)
		fixture.chunks[operation] = chunk
	}
}

func TestOperationWireBuildsCompleteCandidatePreAndPostRetirementSequence(t *testing.T) {
	artifacts := newMigrationGateArtifacts(t, 1)
	bindings := newTestScriptBindingSet(t)
	core, err := artifacts.guard.GuardCore()
	if err != nil {
		t.Fatal(err)
	}
	legacyInput, err := artifacts.legacy.Input()
	if err != nil {
		t.Fatal(err)
	}
	freezeInput, err := artifacts.freeze.Input()
	if err != nil {
		t.Fatal(err)
	}

	runID := core.CandidateRun()
	job := operationWireCandidateSourceJob(t)
	createInput := validCreateRunWireInput(t, SourceV1Migration)
	createInput.RunID = runID
	createInput.ExpectedSeedCount = 1
	createInput.SourceSHA256 = legacyInput.V1SourceSHA256
	runPolicy := newAuthenticatedTestRunPolicyAuthority(
		t, runID, createInput.CrawlPolicySHA256, createInput.RenderPolicySHA256, createInput.PolicyGroups,
	)
	retireInput := RetireLegacyKeysWireInput{
		FreezeNonce: freezeInput.FreezeNonce, BackupSHA256: legacyInput.BackupSHA256,
		V1Count: legacyInput.V1Count, V1URLFieldCount: legacyInput.V1URLFieldCount, V1DepthFieldCount: legacyInput.V1DepthFieldCount,
		V1SourceSHA256: legacyInput.V1SourceSHA256, V1QueueEvidenceSHA256: legacyInput.V1QueueEvidenceSHA256,
		V1URLsEvidenceSHA256: legacyInput.V1URLsEvidenceSHA256, V1DepthsEvidenceSHA256: legacyInput.V1DepthsEvidenceSHA256,
		SpiderQueueType: legacyInput.SpiderQueueType, SpiderQueueCount: legacyInput.SpiderQueueCount,
		SpiderQueueEvidenceSHA256: legacyInput.SpiderQueueEvidenceSHA256, SignalQueueType: legacyInput.SignalQueueType,
		SignalQueueCount: legacyInput.SignalQueueCount, SignalQueueEvidenceSHA256: legacyInput.SignalQueueEvidenceSHA256,
	}

	preRetirement := []struct {
		operation OperationName
		construct func(TransportGate) (OperationWireRequest, error)
	}{
		{OperationCreateRun, func(gate TransportGate) (OperationWireRequest, error) {
			return NewCreateRunWireRequest(gate, createInput)
		}},
		{OperationEnqueueBatch, func(gate TransportGate) (OperationWireRequest, error) {
			return NewEnqueueBatchWireRequest(gate, runPolicy, runID, []SourceJob{job})
		}},
		{OperationBeginRunAudit, func(gate TransportGate) (OperationWireRequest, error) {
			return NewBeginRunAuditWireRequest(gate, runID)
		}},
		{OperationAuditRunBatch, func(gate TransportGate) (OperationWireRequest, error) {
			return NewAuditRunBatchWireRequest(gate, runPolicy, AuditRunBatchWireInput{RunID: runID, Jobs: []SourceJob{job}})
		}},
		{OperationSealRun, func(gate TransportGate) (OperationWireRequest, error) {
			return NewSealRunWireRequest(gate, SealRunWireInput{RunID: runID, ExpectedJobCount: 1, SourceSHA256: legacyInput.V1SourceSHA256})
		}},
		{OperationCancelRun, func(gate TransportGate) (OperationWireRequest, error) {
			return NewCancelRunWireRequest(gate, CancelRunWireInput{RunID: runID, Reason: ReasonSourceCancelled})
		}},
		{OperationCancelBatch, func(gate TransportGate) (OperationWireRequest, error) {
			return NewCancelBatchWireRequest(gate, runID)
		}},
		{OperationPurgeRunBatch, func(gate TransportGate) (OperationWireRequest, error) {
			return NewPurgeRunBatchWireRequest(gate, PurgeRunBatchWireInput{
				RunID: runID, EvidenceSHA256: legacyInput.V1SourceSHA256, ExpectedFirstJobID: job.JobID,
			})
		}},
		{OperationRetireLegacyKeys, func(gate TransportGate) (OperationWireRequest, error) {
			return NewRetireLegacyKeysWireRequest(gate, retireInput)
		}},
	}
	for _, step := range preRetirement {
		t.Run(string(step.operation)+"_pre_retirement", func(t *testing.T) {
			gate, err := NewTransportGate(step.operation, candidateGateInput(artifacts, CandidateBeforeLegacyRetirement, nil))
			if err != nil {
				t.Fatal(err)
			}
			request, err := step.construct(gate)
			if err != nil {
				t.Fatal(err)
			}
			assertCandidateWireBuild(t, bindings, request, false)
		})
	}

	postRetirement := []struct {
		operation OperationName
		construct func(TransportGate) (OperationWireRequest, error)
	}{
		{OperationRetireLegacyKeys, func(gate TransportGate) (OperationWireRequest, error) {
			return NewRetireLegacyKeysWireRequest(gate, retireInput)
		}},
		{OperationPromoteCandidateContracts, func(gate TransportGate) (OperationWireRequest, error) {
			return NewPromoteCandidateContractsWireRequest(gate, PromoteCandidateContractsWireInput{
				FreezeNonce: freezeInput.FreezeNonce, GuardCore: core,
			})
		}},
	}
	for _, step := range postRetirement {
		t.Run(string(step.operation)+"_post_retirement", func(t *testing.T) {
			gate, err := NewTransportGate(step.operation, candidateGateInput(artifacts, CandidateAfterLegacyRetirement, &artifacts.legacy))
			if err != nil {
				t.Fatal(err)
			}
			request, err := step.construct(gate)
			if err != nil {
				t.Fatal(err)
			}
			built := assertCandidateWireBuild(t, bindings, request, true)
			if step.operation == OperationPromoteCandidateContracts {
				runKey, err := RunKey(runID)
				if err != nil {
					t.Fatal(err)
				}
				if !containsWireKey(built.Keys(), runKey) {
					t.Fatal("migration promotion omitted the supplied candidate run keys")
				}
			}
		})
	}
}

func TestOperationWireSensitiveInputsUseFailClosedDiagnosticRedaction(t *testing.T) {
	fixture := newRedactionSurfaceFixture()
	tests := []redactionSurfaceCase{
		{name: "script binding review", typeName: "ScriptBindingReview", value: ScriptBindingReview{}},
		{name: "script binding set", typeName: "ScriptBindingSet", value: ScriptBindingSet{bindings: []scriptBinding{{source: fixture.textRaw}}}},
		{name: "approve boot input", typeName: "ApproveBootWireInput", value: ApproveBootWireInput{CurrentRedisRunID: fixture.textRaw}},
		{name: "install candidate input", typeName: "InstallCandidateMarkersWireInput", value: InstallCandidateMarkersWireInput{FreezeNonce: fixture.textRaw}},
		{name: "retire legacy input", typeName: "RetireLegacyKeysWireInput", value: RetireLegacyKeysWireInput{FreezeNonce: fixture.textRaw}},
		{name: "promote candidate input", typeName: "PromoteCandidateContractsWireInput", value: PromoteCandidateContractsWireInput{FreezeNonce: fixture.textRaw}},
		{name: "planned shutdown input", typeName: "MarkPlannedShutdownWireInput", value: MarkPlannedShutdownWireInput{PlannedShutdownNonce: fixture.textRaw}},
		{name: "create run input", typeName: "CreateRunWireInput", value: CreateRunWireInput{RunID: fixture.runID}},
		{name: "audit batch input", typeName: "AuditRunBatchWireInput", value: AuditRunBatchWireInput{RunID: fixture.runID}},
		{name: "seal run input", typeName: "SealRunWireInput", value: SealRunWireInput{RunID: fixture.runID}},
		{name: "activate run input", typeName: "ActivateRunWireInput", value: ActivateRunWireInput{RunID: fixture.runID}},
		{name: "begin stage input", typeName: "BeginStageWireInput", value: BeginStageWireInput{Lease: fixture.lease, Output: CrawlOutput{Page: fixture.page}, Context: OutputContext{lease: fixture.lease, requestStartsBaseline: 2, requestStartsGeneration: 3}}},
		{name: "seal stage input", typeName: "SealStageWireInput", value: SealStageWireInput{Lease: fixture.lease, Context: OutputContext{lease: fixture.lease, requestStartsBaseline: 2, requestStartsGeneration: 3}}},
		{name: "cancel run input", typeName: "CancelRunWireInput", value: CancelRunWireInput{RunID: fixture.runID}},
		{name: "archive run input", typeName: "ArchiveRunWireInput", value: ArchiveRunWireInput{RunID: fixture.runID}},
		{name: "purge run input", typeName: "PurgeRunBatchWireInput", value: PurgeRunBatchWireInput{RunID: fixture.runID}},
		{name: "clean stage input", typeName: "CleanStageWireInput", value: CleanStageWireInput{ExpectedCommitID: fixture.digest}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.composite = true
			test.rawValues = fixture.allRawValues()
			assertRedactionSurfaces(t, test)
		})
	}
}

func TestScriptBundleCannotSelectAScriptPerRequest(t *testing.T) {
	bindings := newTestScriptBindingSet(t)
	request := newMaintainWireRequest(t)
	built, err := BuildEvalSHARequest(bindings, request)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := bindings.bindingFor(OperationMaintainRateScopes)
	if err != nil {
		t.Fatal(err)
	}
	if built.Operation() != OperationMaintainRateScopes || built.ScriptName() != binding.sourceName ||
		built.ScriptSHA1() != binding.redisSHA1 || built.SourceSHA256() != binding.sourceSHA256 {
		t.Fatal("builder did not select the operation-bound authoritative script")
	}

	tampered := cloneTestScriptBindingSet(bindings)
	maintainIndex := testScriptBindingIndex(t, OperationMaintainRateScopes)
	wrong := tampered.bindings[testScriptBindingIndex(t, OperationRetry)]
	wrong.operation = OperationMaintainRateScopes
	tampered.bindings[maintainIndex] = wrong
	if _, err := BuildEvalSHARequest(tampered, request); !errors.Is(err, ErrInvalidScriptBindingSet) && !errors.Is(err, ErrScriptBindingMismatch) {
		t.Fatalf("cross-operation script binding error = %v", err)
	}
}

func TestOperationWireRejectsWrongMissingExtraAndMalformedKeys(t *testing.T) {
	bindings := newTestScriptBindingSet(t)
	tests := []struct {
		name   string
		mutate func(*OperationWireRequest)
	}{
		{name: "wrong", mutate: func(request *OperationWireRequest) { request.keys[0] = []byte(ContractsActiveKey) }},
		{name: "reordered", mutate: func(request *OperationWireRequest) {
			request.keys[0], request.keys[1] = request.keys[1], request.keys[0]
		}},
		{name: "missing", mutate: func(request *OperationWireRequest) { request.keys = request.keys[:len(request.keys)-1] }},
		{name: "extra", mutate: func(request *OperationWireRequest) {
			request.keys = append(request.keys, []byte("mifolyo:crawl:v2:unexpected"))
		}},
		{name: "malformed", mutate: func(request *OperationWireRequest) { request.keys[0] = []byte("mifolyo:crawl:v2:rate:\n") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := newMaintainWireRequest(t)
			test.mutate(&request)
			if _, err := BuildEvalSHARequest(bindings, request); !errors.Is(err, ErrOperationWireKeys) {
				t.Fatalf("key validation error = %v", err)
			}
		})
	}
}

func TestOperationWireRejectsWrongMissingAndExtraSemanticFields(t *testing.T) {
	bindings := newTestScriptBindingSet(t)
	tests := []struct {
		name   string
		mutate func(*OperationWireRequest)
	}{
		{name: "wrong", mutate: func(request *OperationWireRequest) { request.semantic[0].Name = "caller_selected_offset" }},
		{name: "missing", mutate: func(request *OperationWireRequest) { request.semantic = request.semantic[:0] }},
		{name: "extra", mutate: func(request *OperationWireRequest) {
			request.semantic = append(request.semantic, textField("unrelated", "0"))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := newMaintainWireRequest(t)
			test.mutate(&request)
			if _, err := BuildEvalSHARequest(bindings, request); !errors.Is(err, ErrOperationWireArguments) {
				t.Fatalf("semantic field validation error = %v", err)
			}
		})
	}
}

func TestOperationWireRejectsGateAndCreateRunSourceKindMismatch(t *testing.T) {
	artifacts := newGateArtifacts(t)
	maintain := newMaintainWireRequest(t)
	wrongGate, err := NewTransportGate(OperationRetry, activeGateInput(artifacts))
	if err != nil {
		t.Fatal(err)
	}
	maintain.gate = wrongGate
	if _, err := BuildEvalSHARequest(newTestScriptBindingSet(t), maintain); !errors.Is(err, ErrInvalidTransportGate) {
		t.Fatalf("gate mismatch error = %v", err)
	}

	for _, test := range []struct {
		name       string
		mode       GateMode
		phase      CandidatePhase
		sourceKind SourceKind
	}{
		{name: "candidate cannot claim mongo", mode: GateCandidate, phase: CandidateBeforeLegacyRetirement, sourceKind: SourceMongo},
		{name: "active cannot claim migration", mode: GateActive, sourceKind: SourceV1Migration},
	} {
		t.Run(test.name, func(t *testing.T) {
			var gate TransportGate
			var err error
			if test.mode == GateCandidate {
				gate, err = NewTransportGate(OperationCreateRun, candidateGateInput(artifacts, test.phase, nil))
			} else {
				gate, err = NewTransportGate(OperationCreateRun, activeGateInput(artifacts))
			}
			if err != nil {
				t.Fatal(err)
			}
			input := validCreateRunWireInput(t, test.sourceKind)
			if test.mode == GateCandidate {
				input.ExpectedSeedCount = 1
			}
			if _, err := NewCreateRunWireRequest(gate, input); !errors.Is(err, ErrInvalidSourceKind) {
				t.Fatalf("source kind mismatch error = %v", err)
			}
		})
	}
}

func TestOperationWireDerivesRecordCountAndRejectsRecordByteMismatch(t *testing.T) {
	artifacts := newGateArtifacts(t)
	gate, err := NewTransportGate(OperationCreateRun, activeGateInput(artifacts))
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewCreateRunWireRequest(gate, validCreateRunWireInput(t, SourceMongo))
	if err != nil {
		t.Fatal(err)
	}
	bindings := newTestScriptBindingSet(t)
	built, err := BuildEvalSHARequest(bindings, request)
	if err != nil {
		t.Fatal(err)
	}
	arguments := built.Arguments()
	countIndex := 7 + len(operationWireSpecifications[OperationCreateRun].semanticFields) - 1
	if string(arguments[countIndex]) != "1" {
		t.Fatalf("derived policy_group_count = %q", arguments[countIndex])
	}
	encoded, _ := EncodeRecord(request.records[0])
	if !bytesEqual(arguments[countIndex+1], encoded) {
		t.Fatal("record bytes were not serialized from the typed record")
	}

	tests := []struct {
		name   string
		mutate func(*OperationWireRequest)
	}{
		{name: "count", mutate: func(value *OperationWireRequest) { value.semantic[len(value.semantic)-1].Value = []byte("2") }},
		{name: "record bytes", mutate: func(value *OperationWireRequest) { value.recordBytes[0] = append(value.recordBytes[0], 0) }},
		{name: "missing record", mutate: func(value *OperationWireRequest) { value.records = value.records[:0] }},
		{name: "record field", mutate: func(value *OperationWireRequest) { value.records[0][0].Name = "caller_group" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate, err := NewCreateRunWireRequest(gate, validCreateRunWireInput(t, SourceMongo))
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(&candidate)
			if _, err := BuildEvalSHARequest(bindings, candidate); !errors.Is(err, ErrOperationWireRecords) {
				t.Fatalf("record mismatch error = %v", err)
			}
		})
	}
}

func TestOperationWireRejectsChunkKindAndOrdinalMismatch(t *testing.T) {
	request := newOutlinksStageWireRequest(t)
	bindings := newTestScriptBindingSet(t)
	if _, err := BuildEvalSHARequest(bindings, request); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(*OperationWireRequest)
	}{
		{name: "kind context", mutate: func(value *OperationWireRequest) { value.chunk.kind = ChunkImages }},
		{name: "kind argument", mutate: func(value *OperationWireRequest) { value.semantic[6].Value = []byte(ChunkImages) }},
		{name: "ordinal context", mutate: func(value *OperationWireRequest) { value.chunk.ordinal++ }},
		{name: "ordinal argument", mutate: func(value *OperationWireRequest) { value.semantic[7].Value = []byte("1") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := newOutlinksStageWireRequest(t)
			test.mutate(&candidate)
			if _, err := BuildEvalSHARequest(bindings, candidate); !errors.Is(err, ErrOperationWireChunk) {
				t.Fatalf("chunk mismatch error = %v", err)
			}
		})
	}
}

func TestOperationWireAllRequestSizeBoundaries(t *testing.T) {
	sha := strings.Repeat("a", 40)
	tests := []struct {
		operation   OperationName
		recordCount uint64
		limit       uint64
	}{
		{operation: OperationRetry, limit: MaxOrdinaryEvalSHARequestBytes},
		{operation: OperationStagePageBlob, recordCount: 1, limit: MaxPageBlobEvalSHARequestBytes},
		{operation: OperationStageOutlinksBatch, recordCount: 1, limit: MaxNonBlobStageBatchRequestBytes},
		{operation: OperationCommit, limit: MaxCommitEvalSHARequestBytes},
	}
	for _, test := range tests {
		t.Run(string(test.operation), func(t *testing.T) {
			if operationWireSpecifications[test.operation].requestByteLimit != test.limit {
				t.Fatal("closed specification has the wrong request limit")
			}
			argumentLength := exactSingleArgumentLength(t, sha, test.limit)
			argument := make([]byte, argumentLength)
			size, err := validateEvalSHARequest(test.operation, sha, nil, [][]byte{argument}, test.recordCount)
			if err != nil || size != test.limit {
				t.Fatalf("at limit: size=%d err=%v", size, err)
			}
			argument = append(argument, 0)
			size, err = validateEvalSHARequest(test.operation, sha, nil, [][]byte{argument}, test.recordCount)
			if !errors.Is(err, ErrCommandBoundsExceeded) || size <= test.limit {
				t.Fatalf("over limit: size=%d err=%v", size, err)
			}
		})
	}
}

func TestEvalSHARequestDefensivelyCopiesProductionWireData(t *testing.T) {
	built, err := BuildEvalSHARequest(newTestScriptBindingSet(t), newMaintainWireRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	keys := built.Keys()
	arguments := built.Arguments()
	keys[0][0] = 'X'
	arguments[0][0] = 'X'
	if built.Keys()[0][0] == 'X' || built.Arguments()[0][0] == 'X' {
		t.Fatal("EvalSHA request exposed mutable wire data")
	}
}

func newMaintainWireRequest(t *testing.T) OperationWireRequest {
	t.Helper()
	artifacts := newGateArtifacts(t)
	gate, err := NewTransportGate(OperationMaintainRateScopes, activeGateInput(artifacts))
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewMaintainRateScopesWireRequest(gate, 0)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func assertCandidateWireBuild(
	t *testing.T,
	bindings ScriptBindingSet,
	request OperationWireRequest,
	legacyExpected bool,
) EvalSHARequest {
	t.Helper()
	built, err := BuildEvalSHARequest(bindings, request)
	if err != nil {
		t.Fatal(err)
	}
	arguments := built.Arguments()
	if built.Operation() != request.Operation() || len(arguments) < 7 || string(arguments[0]) != string(GateCandidate) {
		t.Fatal("candidate request did not retain its closed operation and gate")
	}
	if (len(arguments[5]) != 0) != legacyExpected {
		t.Fatalf("candidate legacy evidence presence = %t, want %t", len(arguments[5]) != 0, legacyExpected)
	}
	return built
}

func containsWireKey(keys [][]byte, expected string) bool {
	for _, key := range keys {
		if string(key) == expected {
			return true
		}
	}
	return false
}

func operationWireCandidateSourceJob(t *testing.T) SourceJob {
	t.Helper()
	identity, err := requireCanonicalURL("https://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	jobID, err := ParseJobID(identity.URLID)
	if err != nil {
		t.Fatal(err)
	}
	score, err := ParseScoreText("0")
	if err != nil {
		t.Fatal(err)
	}
	rateScopeID := RateScopeID(strings.Repeat("2", 32))
	target := RequestTarget{URLID: jobID, CanonicalURL: identity.CanonicalURL}
	decision, err := NewPolicyDecision(PolicyDecisionInput{
		RequestKind: RequestDocument, Target: target, GroupID: "default", RateScopeID: rateScopeID,
		GroupConcurrency: 1, OriginConcurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return SourceJob{
		JobID: jobID, CanonicalURL: identity.CanonicalURL, ScoreText: score,
		GroupID: "default", RateScopeID: rateScopeID, Decision: decision,
	}
}

func validCreateRunWireInput(t *testing.T, sourceKind SourceKind) CreateRunWireInput {
	t.Helper()
	digest := Digest(strings.Repeat("a", 64))
	rateScopeID := RateScopeID(strings.Repeat("2", 32))
	groupScopeID, err := DeriveGroupScopeID(rateScopeID)
	if err != nil {
		t.Fatal(err)
	}
	groups := []PolicyGroup{{
		GroupID: "default", RateScopeID: rateScopeID, GroupScopeID: groupScopeID,
		RequestStartLimit: MaxRequestStartsPerGroup, Concurrency: 1, IntervalMS: 0,
	}}
	groupDigest, err := DerivePolicyGroupMapDigest(groups)
	if err != nil {
		t.Fatal(err)
	}
	return CreateRunWireInput{
		RunID: RunID(strings.Repeat("1", 32)), SourceKind: sourceKind, SourceSHA256: digest,
		ExpectedSeedCount: 0, AuthorizationSHA256: digest, AuthorizationScopeSHA256: digest,
		AuthorizationExpiresAtMS: 1, CanonicalizationVersion: 1, CanonicalizationSHA256: digest,
		CrawlPolicyVersion: 2, CrawlPolicySHA256: digest, RenderPolicyVersion: 1, RenderPolicySHA256: digest,
		PolicyGroupMapSHA256: groupDigest, MaxJobs: MaxJobsPerRun, MaxRequestStarts: MaxRequestStartsPerRun,
		GlobalConcurrencyLimit: GlobalActiveRequestLimit, MaxDeliveryAttempts: MaxDeliveryAttempts, PolicyGroups: groups,
	}
}

func newOutlinksStageWireRequest(t *testing.T) OperationWireRequest {
	t.Helper()
	artifacts := newGateArtifacts(t)
	gate, err := NewTransportGate(OperationStageOutlinksBatch, activeGateInput(artifacts))
	if err != nil {
		t.Fatal(err)
	}
	fixture := newWireOracleFixture(t)
	chunk, err := NewOutlinksStageChunk(fixture.commitIdentity, 0, fixture.outputContext, []string{"https://example.com/outlink"})
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewStageOutlinksBatchWireRequest(gate, fixture.lease, chunk)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func equalGateModes(left, right []GateMode) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func bytesEqual(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
