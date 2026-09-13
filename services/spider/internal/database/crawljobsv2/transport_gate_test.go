package crawljobsv2

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestTransportGateModelsCompleteCandidatePreAndPostRetirementSequence(t *testing.T) {
	artifacts := newGateArtifacts(t)

	boot, err := NewTransportGate(OperationInstallCandidateMarkers, TransportGateInput{
		Mode: GateBootOnly, BootEpoch: artifacts.bootEpoch,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertGateArguments(t, boot, GateBootOnly, false, false, false, false)

	preRetirementOperations := []OperationName{
		OperationCreateRun,
		OperationEnqueueBatch,
		OperationBeginRunAudit,
		OperationAuditRunBatch,
		OperationSealRun,
		OperationCancelRun,
		OperationCancelBatch,
		OperationPurgeRunBatch,
		OperationRetireLegacyKeys,
	}
	for _, operation := range preRetirementOperations {
		t.Run(string(operation)+"_before_retirement", func(t *testing.T) {
			gate, err := NewTransportGate(operation, candidateGateInput(artifacts, CandidateBeforeLegacyRetirement, nil))
			if err != nil {
				t.Fatal(err)
			}
			assertGateArguments(t, gate, GateCandidate, true, true, false, true)
		})
	}

	for _, operation := range []OperationName{OperationRetireLegacyKeys, OperationPromoteCandidateContracts} {
		t.Run(string(operation)+"_after_retirement", func(t *testing.T) {
			gate, err := NewTransportGate(operation, candidateGateInput(artifacts, CandidateAfterLegacyRetirement, &artifacts.legacy))
			if err != nil {
				t.Fatal(err)
			}
			assertGateArguments(t, gate, GateCandidate, true, true, true, true)
		})
	}

	rejections := []struct {
		name      string
		operation OperationName
		phase     CandidatePhase
		legacy    *LegacyRetirementRecord
	}{
		{name: "run creation cannot require retirement first", operation: OperationCreateRun, phase: CandidateBeforeLegacyRetirement, legacy: &artifacts.legacy},
		{name: "run operation cannot continue after retirement", operation: OperationCreateRun, phase: CandidateAfterLegacyRetirement, legacy: &artifacts.legacy},
		{name: "retire post-state requires exact evidence", operation: OperationRetireLegacyKeys, phase: CandidateAfterLegacyRetirement},
		{name: "promotion cannot run before retirement", operation: OperationPromoteCandidateContracts, phase: CandidateBeforeLegacyRetirement},
		{name: "promotion requires exact evidence", operation: OperationPromoteCandidateContracts, phase: CandidateAfterLegacyRetirement},
		{name: "candidate phase is mandatory", operation: OperationCreateRun},
	}
	for _, test := range rejections {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewTransportGate(test.operation, candidateGateInput(artifacts, test.phase, test.legacy))
			if err == nil {
				t.Fatal("invalid candidate sequence was accepted")
			}
		})
	}
}

func TestTransportGateActiveAuthorityBindsConfigAndCutoverSemantics(t *testing.T) {
	t.Run("fresh", func(t *testing.T) {
		artifacts := newGateArtifacts(t)
		gate, err := NewTransportGate(OperationRetry, activeGateInput(artifacts))
		if err != nil {
			t.Fatal(err)
		}
		assertGateArguments(t, gate, GateActive, true, true, true, false)
	})

	t.Run("v1 migration", func(t *testing.T) {
		artifacts := newMigrationGateArtifacts(t, 3)
		if _, err := NewTransportGate(OperationRetry, activeGateInput(artifacts)); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("fresh rejects positive v1 count", func(t *testing.T) {
		fresh := newGateArtifacts(t)
		migration := newMigrationGateArtifacts(t, 3)
		fresh.legacy = migration.legacy
		if _, err := NewTransportGate(OperationRetry, activeGateInput(fresh)); !errors.Is(err, ErrArtifactMismatch) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("migration rejects zero v1 count", func(t *testing.T) {
		migration := newMigrationGateArtifacts(t, 3)
		migration.legacy = newGateArtifacts(t).legacy
		if _, err := NewTransportGate(OperationRetry, activeGateInput(migration)); !errors.Is(err, ErrArtifactMismatch) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("redis config mismatch", func(t *testing.T) {
		artifacts := newGateArtifactsWithConfigs(t, Digest(strings.Repeat("e", 64)), Digest(strings.Repeat("f", 64)))
		if _, err := NewTransportGate(OperationRetry, activeGateInput(artifacts)); !errors.Is(err, ErrArtifactMismatch) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("contract mismatch", func(t *testing.T) {
		artifacts := newGateArtifacts(t)
		input := activeGateInput(artifacts)
		input.Contract = Digest(strings.Repeat("b", 64))
		if _, err := NewTransportGate(OperationRetry, input); !errors.Is(err, ErrArtifactMismatch) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestTransportGateDefensivelyCopiesAndRevalidatesEncodedAuthority(t *testing.T) {
	artifacts := newGateArtifacts(t)
	gate, err := NewTransportGate(OperationRetry, activeGateInput(artifacts))
	if err != nil {
		t.Fatal(err)
	}
	first, _ := gate.Arguments()
	first[0][0] = 'X'
	second, _ := gate.Arguments()
	if bytes.Equal(first[0], second[0]) || string(second[0]) != string(GateActive) {
		t.Fatal("transport gate exposed mutable backing data")
	}

	tampered := gate
	tampered.arguments[3] = append([]byte(nil), tampered.arguments[3]...)
	tampered.arguments[3][len(tampered.arguments[3])-1] ^= 1
	if _, err := tampered.Arguments(); err == nil {
		t.Fatal("tampered encoded authority was accepted")
	}
}

type gateArtifacts struct {
	bootEpoch string
	contract  Digest
	marker    CompatibilityMarker
	guard     StoredCommitGuard
	legacy    LegacyRetirementRecord
	freeze    AdminFreezeRecord
}

func newGateArtifacts(t *testing.T) gateArtifacts {
	t.Helper()
	digest := Digest(strings.Repeat("a", 64))
	return newGateArtifactsFor(t, CutoverFresh, 0, digest, digest)
}

func newMigrationGateArtifacts(t *testing.T, v1Count uint64) gateArtifacts {
	t.Helper()
	digest := Digest(strings.Repeat("a", 64))
	return newGateArtifactsFor(t, CutoverV1Migration, v1Count, digest, digest)
}

func newGateArtifactsWithConfigs(t *testing.T, artifactConfig, coreConfig Digest) gateArtifacts {
	t.Helper()
	return newGateArtifactsFor(t, CutoverFresh, 0, artifactConfig, coreConfig)
}

func newGateArtifactsFor(t *testing.T, cutover CutoverMode, v1Count uint64, artifactConfig, coreConfig Digest) gateArtifacts {
	t.Helper()
	digest := Digest(strings.Repeat("a", 64))
	image := ImageDigest("sha256:" + strings.Repeat("b", 64))
	candidateRunID := RunID("")
	if cutover == CutoverV1Migration {
		candidateRunID = RunID(strings.Repeat("1", 32))
	}
	core, err := NewGuardCore(GuardCoreInput{
		ContractSHA256: digest, RedisVersion: "7.2.5", RedisConfigSHA256: coreConfig,
		MaximumShapeSHA256: digest, MemoryFixtureSHA256: digest, LuaBenchmarkSHA256: digest,
		AOFCrashEvidenceSHA256: digest, CutoverMode: cutover, CandidateRunID: candidateRunID,
	})
	if err != nil {
		t.Fatal(err)
	}
	coreDigest, err := core.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := NewCompatibilityArtifact(CompatibilityArtifactInput{
		RedisConfigSHA256: artifactConfig, CommitGuardSHA256: coreDigest,
		SpiderImage: image, SeedImporterImage: image, CrawlAdminImage: image, IndexerImage: image,
		ImageIndexerImage: image, BacklinksProcessorImage: image, MonitoringImage: image,
		RenderWorkerImage: "disabled",
	})
	if err != nil {
		t.Fatal(err)
	}
	marker, err := NewCompatibilityMarker(artifact)
	if err != nil {
		t.Fatal(err)
	}
	manifestDigest, err := marker.ManifestSHA256()
	if err != nil {
		t.Fatal(err)
	}
	guard, err := NewStoredCommitGuard(core, manifestDigest, 1)
	if err != nil {
		t.Fatal(err)
	}
	freeze, err := NewAdminFreezeRecord(AdminFreezeRecordInput{
		FreezeNonce: strings.Repeat("c", 32), ProcessStopEvidenceSHA256: digest,
		CandidateManifestSHA256: manifestDigest, CandidateContractSHA256: digest, CreatedAtMS: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	deletedBitmap := "00000"
	if v1Count > 0 {
		deletedBitmap = "11100"
	}
	legacy, err := NewLegacyRetirementRecord(LegacyRetirementRecordInput{
		FreezeNonce: strings.Repeat("c", 32), BackupSHA256: digest,
		V1Count: v1Count, V1URLFieldCount: v1Count, V1DepthFieldCount: v1Count,
		V1SourceSHA256: digest, V1QueueEvidenceSHA256: digest, V1URLsEvidenceSHA256: digest, V1DepthsEvidenceSHA256: digest,
		SpiderQueueType: LegacyTypeNone, SpiderQueueEvidenceSHA256: digest,
		SignalQueueType: LegacyTypeNone, SignalQueueEvidenceSHA256: digest,
		DeletedBitmap: deletedBitmap, RetiredAtMS: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	return gateArtifacts{
		bootEpoch: strings.Repeat("d", 32), contract: digest, marker: marker,
		guard: guard, legacy: legacy, freeze: freeze,
	}
}

func candidateGateInput(artifacts gateArtifacts, phase CandidatePhase, legacy *LegacyRetirementRecord) TransportGateInput {
	return TransportGateInput{
		Mode: GateCandidate, CandidatePhase: phase, BootEpoch: artifacts.bootEpoch, Contract: artifacts.contract,
		Compatibility: &artifacts.marker, Legacy: legacy, AdminFreeze: &artifacts.freeze,
	}
}

func activeGateInput(artifacts gateArtifacts) TransportGateInput {
	return TransportGateInput{
		Mode: GateActive, BootEpoch: artifacts.bootEpoch, Contract: artifacts.contract,
		Compatibility: &artifacts.marker, CommitGuard: &artifacts.guard, Legacy: &artifacts.legacy,
	}
}

func assertGateArguments(t *testing.T, gate TransportGate, mode GateMode, contract, compatibility, legacy, freeze bool) {
	t.Helper()
	arguments, err := gate.Arguments()
	if err != nil || len(arguments) != 7 || string(arguments[0]) != string(mode) {
		t.Fatalf("gate arguments invalid: len=%d err=%v", len(arguments), err)
	}
	wants := []bool{true, true, contract, compatibility, mode == GateActive, legacy, freeze}
	for index, want := range wants {
		if (len(arguments[index]) > 0) != want {
			t.Fatalf("gate argument %d presence = %t, want %t", index, len(arguments[index]) > 0, want)
		}
	}
}
