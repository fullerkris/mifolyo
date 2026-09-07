package crawljobsv2

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type testWireRequest struct{ shape operationWireShape }

func (request testWireRequest) crawlJobsV2WireShape() operationWireShape { return request.shape }

func TestTransportGateEnforcesModeArtifactsAndCandidateSource(t *testing.T) {
	artifacts := newGateArtifacts(t)

	boot, err := NewTransportGate(OperationInstallCandidateMarkers, TransportGateInput{
		Mode: GateBootOnly, BootEpoch: artifacts.bootEpoch,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertGateArguments(t, boot, GateBootOnly, false, false, false, false)

	candidate, err := NewTransportGate(OperationCreateRun, TransportGateInput{
		Mode: GateCandidate, BootEpoch: artifacts.bootEpoch, Contract: artifacts.contract,
		Compatibility: &artifacts.marker, Legacy: &artifacts.legacy, AdminFreeze: &artifacts.freeze,
		SourceKind: SourceV1Migration,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertGateArguments(t, candidate, GateCandidate, true, true, true, true)

	if _, err := NewTransportGate(OperationCreateRun, TransportGateInput{
		Mode: GateCandidate, BootEpoch: artifacts.bootEpoch, Contract: artifacts.contract,
		Compatibility: &artifacts.marker, Legacy: &artifacts.legacy, AdminFreeze: &artifacts.freeze,
		SourceKind: SourceMongo,
	}); !errors.Is(err, ErrInvalidSourceKind) {
		t.Fatalf("candidate Mongo source error = %v", err)
	}

	active, err := NewTransportGate(OperationCreateRun, TransportGateInput{
		Mode: GateActive, BootEpoch: artifacts.bootEpoch, Contract: artifacts.contract,
		Compatibility: &artifacts.marker, CommitGuard: &artifacts.guard, Legacy: &artifacts.legacy,
		SourceKind: SourceMongo,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertGateArguments(t, active, GateActive, true, true, true, false)

	if _, err := NewTransportGate(OperationCreateRun, TransportGateInput{
		Mode: GateActive, BootEpoch: artifacts.bootEpoch, Contract: artifacts.contract,
		Compatibility: &artifacts.marker, CommitGuard: &artifacts.guard, Legacy: &artifacts.legacy,
		SourceKind: SourceV1Migration,
	}); !errors.Is(err, ErrInvalidSourceKind) {
		t.Fatalf("active migration source error = %v", err)
	}
}

func TestTransportGateRejectsCrossArtifactMismatchAndDefensivelyCopies(t *testing.T) {
	artifacts := newGateArtifacts(t)
	otherDigest := Digest(strings.Repeat("b", 64))
	if _, err := NewTransportGate(OperationRetry, TransportGateInput{
		Mode: GateActive, BootEpoch: artifacts.bootEpoch, Contract: otherDigest,
		Compatibility: &artifacts.marker, CommitGuard: &artifacts.guard, Legacy: &artifacts.legacy,
	}); !errors.Is(err, ErrArtifactMismatch) {
		t.Fatalf("cross-artifact mismatch error = %v", err)
	}

	gate, err := NewTransportGate(OperationRetry, TransportGateInput{
		Mode: GateActive, BootEpoch: artifacts.bootEpoch, Contract: artifacts.contract,
		Compatibility: &artifacts.marker, CommitGuard: &artifacts.guard, Legacy: &artifacts.legacy,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := gate.Arguments()
	first[0][0] = 'X'
	second, _ := gate.Arguments()
	if bytes.Equal(first[0], second[0]) || string(second[0]) != string(GateActive) {
		t.Fatal("transport gate exposed mutable backing data")
	}
}

func TestTypedEvalSHARequestDerivesRecordCountAndCopiesWireData(t *testing.T) {
	sha := strings.Repeat("a", 40)
	records := make([]Record, MaxAliasesPerJob)
	for index := range records {
		records[index] = Record{textField("value", canonicalDecimal(uint64(index)))}
	}
	request, err := BuildEvalSHARequest(sha, testWireRequest{shape: operationWireShape{
		operation: OperationStageAliasesBatch,
		keys:      [][]byte{[]byte("key")}, arguments: [][]byte{[]byte("argument")}, records: records,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if request.Operation() != OperationStageAliasesBatch || request.SerializedSize() == 0 {
		t.Fatal("typed request identity was not retained")
	}
	keys := request.Keys()
	keys[0][0] = 'X'
	if string(request.Keys()[0]) != "key" {
		t.Fatal("typed request exposed mutable keys")
	}
	records = append(records, Record{})
	if _, err := BuildEvalSHARequest(sha, testWireRequest{shape: operationWireShape{
		operation: OperationStageAliasesBatch, records: records,
	}}); !errors.Is(err, ErrRecordBoundsExceeded) {
		t.Fatalf("derived over-limit record count error = %v", err)
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
	image := ImageDigest("sha256:" + strings.Repeat("b", 64))
	core, err := NewGuardCore(GuardCoreInput{
		ContractSHA256: digest, RedisVersion: "7.2.5", RedisConfigSHA256: digest,
		MaximumShapeSHA256: digest, MemoryFixtureSHA256: digest, LuaBenchmarkSHA256: digest,
		AOFCrashEvidenceSHA256: digest, CutoverMode: CutoverFresh,
	})
	if err != nil {
		t.Fatal(err)
	}
	coreDigest, err := core.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := NewCompatibilityArtifact(CompatibilityArtifactInput{
		RedisConfigSHA256: digest, CommitGuardSHA256: coreDigest,
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
	legacy, err := NewLegacyRetirementRecord(LegacyRetirementRecordInput{
		FreezeNonce: strings.Repeat("c", 32), BackupSHA256: digest, V1SourceSHA256: digest,
		V1QueueEvidenceSHA256: digest, V1URLsEvidenceSHA256: digest, V1DepthsEvidenceSHA256: digest,
		SpiderQueueType: LegacyTypeNone, SpiderQueueEvidenceSHA256: digest,
		SignalQueueType: LegacyTypeNone, SignalQueueEvidenceSHA256: digest,
		DeletedBitmap: "00000", RetiredAtMS: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	return gateArtifacts{
		bootEpoch: strings.Repeat("d", 32), contract: digest, marker: marker,
		guard: guard, legacy: legacy, freeze: freeze,
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
