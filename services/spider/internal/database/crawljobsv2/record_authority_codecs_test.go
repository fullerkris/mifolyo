package crawljobsv2

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type recordAuthorityCodecCase struct {
	name      string
	schema    RecordSchema
	record    Record
	encoded   []byte
	roundTrip func([]byte) ([]byte, error)
}

func TestRecordAuthorityDirectCodecRoundTripsAndRejectsBadFrames(t *testing.T) {
	cases := recordAuthorityCodecCases(t)
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			roundTripped, err := testCase.roundTrip(testCase.encoded)
			if err != nil {
				t.Fatalf("decode failed: %v", err)
			}
			if !bytes.Equal(roundTripped, testCase.encoded) {
				t.Fatal("codec round trip changed exact bytes")
			}
			if err := ValidateRecord(testCase.schema, testCase.record); err != nil {
				t.Fatalf("ValidateRecord rejected constructor output: %v", err)
			}

			wrongField := cloneRecord(testCase.record)
			wrongField[0].Name = "wrong_field"
			wrongFieldBytes, err := EncodeRecord(wrongField)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := testCase.roundTrip(wrongFieldBytes); !errors.Is(err, ErrInvalidRecordEncoding) {
				t.Fatalf("wrong-field decode error = %v", err)
			}
			if err := ValidateRecord(testCase.schema, wrongField); !errors.Is(err, ErrInvalidSchema) {
				t.Fatalf("wrong-field ValidateRecord error = %v", err)
			}

			if _, err := testCase.roundTrip(testCase.encoded[:len(testCase.encoded)-1]); err == nil {
				t.Fatal("truncated encoding was accepted")
			}
			withTrailingByte := append(append([]byte(nil), testCase.encoded...), 0)
			if _, err := testCase.roundTrip(withTrailingByte); !errors.Is(err, ErrInvalidRecordEncoding) {
				t.Fatalf("trailing-byte decode error = %v", err)
			}
		})
	}
}

func TestRecordAuthorityProductionConstructorsRejectZeroSHA256(t *testing.T) {
	bundle := recordAuthorityBundleForTest(t)
	zeroImage := ImageDigest("sha256:" + ZeroSHA256)

	compatibilityMutations := []struct {
		name   string
		mutate func(*CompatibilityArtifactInput)
	}{
		{name: "redis config", mutate: func(input *CompatibilityArtifactInput) { input.RedisConfigSHA256 = Digest(ZeroSHA256) }},
		{name: "commit guard", mutate: func(input *CompatibilityArtifactInput) { input.CommitGuardSHA256 = Digest(ZeroSHA256) }},
		{name: "spider image", mutate: func(input *CompatibilityArtifactInput) { input.SpiderImage = zeroImage }},
		{name: "seed importer image", mutate: func(input *CompatibilityArtifactInput) { input.SeedImporterImage = zeroImage }},
		{name: "crawl admin image", mutate: func(input *CompatibilityArtifactInput) { input.CrawlAdminImage = zeroImage }},
		{name: "indexer image", mutate: func(input *CompatibilityArtifactInput) { input.IndexerImage = zeroImage }},
		{name: "image indexer image", mutate: func(input *CompatibilityArtifactInput) { input.ImageIndexerImage = zeroImage }},
		{name: "backlinks processor image", mutate: func(input *CompatibilityArtifactInput) { input.BacklinksProcessorImage = zeroImage }},
		{name: "monitoring image", mutate: func(input *CompatibilityArtifactInput) { input.MonitoringImage = zeroImage }},
		{name: "render worker image", mutate: func(input *CompatibilityArtifactInput) { input.RenderWorkerImage = string(zeroImage) }},
	}
	for _, mutation := range compatibilityMutations {
		t.Run("compatibility "+mutation.name, func(t *testing.T) {
			input := bundle.compatibilityInput
			mutation.mutate(&input)
			if _, err := NewCompatibilityArtifact(input); !errors.Is(err, ErrInvalidRecordValue) {
				t.Fatalf("zero sentinel error = %v", err)
			}
		})
	}

	guardMutations := []struct {
		name   string
		mutate func(*GuardCoreInput)
	}{
		{name: "contract", mutate: func(input *GuardCoreInput) { input.ContractSHA256 = Digest(ZeroSHA256) }},
		{name: "redis config", mutate: func(input *GuardCoreInput) { input.RedisConfigSHA256 = Digest(ZeroSHA256) }},
		{name: "maximum shape", mutate: func(input *GuardCoreInput) { input.MaximumShapeSHA256 = Digest(ZeroSHA256) }},
		{name: "memory fixture", mutate: func(input *GuardCoreInput) { input.MemoryFixtureSHA256 = Digest(ZeroSHA256) }},
		{name: "Lua benchmark", mutate: func(input *GuardCoreInput) { input.LuaBenchmarkSHA256 = Digest(ZeroSHA256) }},
		{name: "AOF crash evidence", mutate: func(input *GuardCoreInput) { input.AOFCrashEvidenceSHA256 = Digest(ZeroSHA256) }},
	}
	for _, mutation := range guardMutations {
		t.Run("guard "+mutation.name, func(t *testing.T) {
			input := bundle.guardInput
			mutation.mutate(&input)
			if _, err := NewGuardCore(input); !errors.Is(err, ErrInvalidRecordValue) {
				t.Fatalf("zero sentinel error = %v", err)
			}
		})
	}

	if _, err := NewStoredCommitGuard(bundle.guard, Digest(ZeroSHA256), 300); !errors.Is(err, ErrInvalidRecordValue) {
		t.Fatalf("stored manifest zero error = %v", err)
	}

	legacyMutations := []struct {
		name   string
		mutate func(*LegacyRetirementRecordInput)
	}{
		{name: "backup", mutate: func(input *LegacyRetirementRecordInput) { input.BackupSHA256 = Digest(ZeroSHA256) }},
		{name: "source", mutate: func(input *LegacyRetirementRecordInput) { input.V1SourceSHA256 = Digest(ZeroSHA256) }},
		{name: "queue", mutate: func(input *LegacyRetirementRecordInput) { input.V1QueueEvidenceSHA256 = Digest(ZeroSHA256) }},
		{name: "URLs", mutate: func(input *LegacyRetirementRecordInput) { input.V1URLsEvidenceSHA256 = Digest(ZeroSHA256) }},
		{name: "depths", mutate: func(input *LegacyRetirementRecordInput) { input.V1DepthsEvidenceSHA256 = Digest(ZeroSHA256) }},
		{name: "spider queue", mutate: func(input *LegacyRetirementRecordInput) { input.SpiderQueueEvidenceSHA256 = Digest(ZeroSHA256) }},
		{name: "signal queue", mutate: func(input *LegacyRetirementRecordInput) { input.SignalQueueEvidenceSHA256 = Digest(ZeroSHA256) }},
	}
	for _, mutation := range legacyMutations {
		t.Run("legacy "+mutation.name, func(t *testing.T) {
			input := bundle.legacyInput
			mutation.mutate(&input)
			if _, err := NewLegacyRetirementRecord(input); !errors.Is(err, ErrInvalidRecordValue) {
				t.Fatalf("zero sentinel error = %v", err)
			}
		})
	}

	adminMutations := []struct {
		name   string
		mutate func(*AdminFreezeRecordInput)
	}{
		{name: "process stop", mutate: func(input *AdminFreezeRecordInput) { input.ProcessStopEvidenceSHA256 = Digest(ZeroSHA256) }},
		{name: "candidate manifest", mutate: func(input *AdminFreezeRecordInput) { input.CandidateManifestSHA256 = Digest(ZeroSHA256) }},
		{name: "candidate contract", mutate: func(input *AdminFreezeRecordInput) { input.CandidateContractSHA256 = Digest(ZeroSHA256) }},
	}
	for _, mutation := range adminMutations {
		t.Run("admin "+mutation.name, func(t *testing.T) {
			input := bundle.adminInput
			mutation.mutate(&input)
			if _, err := NewAdminFreezeRecord(input); !errors.Is(err, ErrInvalidRecordValue) {
				t.Fatalf("zero sentinel error = %v", err)
			}
		})
	}

	durability := bundle.durabilityInput
	durability.RehearsalEvidenceSHA256 = Digest(ZeroSHA256)
	if _, err := NewDurabilityRecord(durability); !errors.Is(err, ErrInvalidRecordValue) {
		t.Fatalf("rehearsal evidence zero error = %v", err)
	}
	planned := bundle.durabilityInput
	planned.BootState = BootPlanned
	planned.PlannedShutdownNonce = strings.Repeat("9", 32)
	planned.PlannedShutdownEvidenceSHA256 = Digest(ZeroSHA256)
	if _, err := NewDurabilityRecord(planned); !errors.Is(err, ErrInvalidRecordValue) {
		t.Fatalf("planned evidence zero error = %v", err)
	}
	if _, err := NewDurabilityRecord(bundle.durabilityInput); err != nil {
		t.Fatalf("permitted empty optional durability fields rejected: %v", err)
	}
	if _, err := NewFinalPageRecord(OutputContext{}, OutputPage{}, Digest(ZeroSHA256)); !errors.Is(err, ErrInvalidRecordValue) {
		t.Fatalf("final page publication zero error = %v", err)
	}
	if _, err := NewFinalImageRecord(Digest(ZeroSHA256), "", OutputImage{}); !errors.Is(err, ErrInvalidRecordValue) {
		t.Fatalf("final image publication zero error = %v", err)
	}
	if _, err := NewImageManifestRecord(Digest(ZeroSHA256), "", nil); !errors.Is(err, ErrInvalidRecordValue) {
		t.Fatalf("image manifest publication zero error = %v", err)
	}
}

func TestRecordAuthorityProductionDecodersRejectZeroSHA256(t *testing.T) {
	bundle := recordAuthorityBundleForTest(t)
	tests := []struct {
		name   string
		schema RecordSchema
		record func() Record
		index  int
		decode func([]byte) error
	}{
		{
			name: "compatibility Redis config", schema: SchemaCompatibilityArtifact,
			record: func() Record { record, _ := bundle.compatibility.Record(); return record }, index: 10,
			decode: func(encoded []byte) error { _, err := DecodeCompatibilityArtifact(encoded); return err },
		},
		{
			name: "guard evidence", schema: SchemaGuardCore,
			record: func() Record { record, _ := bundle.guard.Record(); return record }, index: 4,
			decode: func(encoded []byte) error { _, err := DecodeGuardCore(encoded); return err },
		},
		{
			name: "stored compatibility manifest", schema: SchemaCommitGuard,
			record: func() Record { record, _ := bundle.storedGuard.Record(); return record }, index: 2,
			decode: func(encoded []byte) error { _, err := DecodeStoredCommitGuard(encoded); return err },
		},
		{
			name: "legacy evidence", schema: SchemaLegacyRetirement,
			record: func() Record { record, _ := bundle.legacy.Record(); return record }, index: 2,
			decode: func(encoded []byte) error { _, err := DecodeLegacyRetirementRecord(encoded); return err },
		},
		{
			name: "admin evidence", schema: SchemaAdminFreeze,
			record: func() Record { record, _ := bundle.admin.Record(); return record }, index: 2,
			decode: func(encoded []byte) error { _, err := DecodeAdminFreezeRecord(encoded); return err },
		},
		{
			name: "durability evidence", schema: SchemaDurability,
			record: func() Record { record, _ := bundle.durability.Record(); return record }, index: 9,
			decode: func(encoded []byte) error { _, err := DecodeDurabilityRecord(encoded); return err },
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			record := testCase.record()
			recordAuthoritySet(record, testCase.index, ZeroSHA256)
			encoded, err := EncodeRecord(record)
			if err != nil {
				t.Fatal(err)
			}
			if err := testCase.decode(encoded); err == nil {
				t.Fatal("production decoder accepted ZERO_SHA256")
			}
			if err := ValidateRecord(testCase.schema, record); err == nil {
				t.Fatal("ValidateRecord accepted ZERO_SHA256")
			}
		})
	}
}

func TestRecordAuthorityProvisionalGuardIsFixtureOnly(t *testing.T) {
	bundle := recordAuthorityBundleForTest(t)
	allowedZeroFields := []struct {
		name   string
		mutate func(*GuardCoreInput)
	}{
		{name: "maximum shape", mutate: func(input *GuardCoreInput) { input.MaximumShapeSHA256 = Digest(ZeroSHA256) }},
		{name: "memory fixture", mutate: func(input *GuardCoreInput) { input.MemoryFixtureSHA256 = Digest(ZeroSHA256) }},
		{name: "Lua benchmark", mutate: func(input *GuardCoreInput) { input.LuaBenchmarkSHA256 = Digest(ZeroSHA256) }},
		{name: "AOF crash evidence", mutate: func(input *GuardCoreInput) { input.AOFCrashEvidenceSHA256 = Digest(ZeroSHA256) }},
	}
	for _, field := range allowedZeroFields {
		t.Run(field.name, func(t *testing.T) {
			input := bundle.guardInput
			field.mutate(&input)
			provisional, err := NewProvisionalGuardCore(input)
			if err != nil {
				t.Fatalf("provisional guard rejected: %v", err)
			}
			record, err := provisional.Record()
			if err != nil || !hasFieldNames(record, guardCoreFieldNames()...) {
				t.Fatalf("provisional shape mismatch: %v", err)
			}
			encoded, err := provisional.Encode()
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeProvisionalGuardCore(encoded)
			if err != nil {
				t.Fatalf("provisional decode failed: %v", err)
			}
			roundTripped, err := decoded.Encode()
			if err != nil || !bytes.Equal(roundTripped, encoded) {
				t.Fatalf("provisional round trip failed: %v", err)
			}
			if _, err := DecodeGuardCore(encoded); !errors.Is(err, ErrInvalidRecordValue) {
				t.Fatalf("production guard decoder accepted provisional bytes: %v", err)
			}
			if err := ValidateRecord(SchemaGuardCore, record); !errors.Is(err, ErrInvalidRecordValue) {
				t.Fatalf("runtime authority validation accepted provisional record: %v", err)
			}
		})
	}

	if _, err := NewProvisionalGuardCore(bundle.guardInput); !errors.Is(err, ErrInvalidRecordValue) {
		t.Fatalf("all-nonzero provisional error = %v", err)
	}
	for _, mutation := range []struct {
		name   string
		mutate func(*GuardCoreInput)
	}{
		{name: "zero contract", mutate: func(input *GuardCoreInput) {
			input.ContractSHA256 = Digest(ZeroSHA256)
			input.MaximumShapeSHA256 = Digest(ZeroSHA256)
		}},
		{name: "zero Redis config", mutate: func(input *GuardCoreInput) {
			input.RedisConfigSHA256 = Digest(ZeroSHA256)
			input.MaximumShapeSHA256 = Digest(ZeroSHA256)
		}},
		{name: "migration mode", mutate: func(input *GuardCoreInput) {
			input.MaximumShapeSHA256 = Digest(ZeroSHA256)
			input.CutoverMode = CutoverV1Migration
			input.CandidateRunID = RunID(strings.Repeat("a", 32))
		}},
		{name: "candidate run", mutate: func(input *GuardCoreInput) {
			input.MaximumShapeSHA256 = Digest(ZeroSHA256)
			input.CandidateRunID = RunID(strings.Repeat("a", 32))
		}},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			input := bundle.guardInput
			mutation.mutate(&input)
			if _, err := NewProvisionalGuardCore(input); err == nil {
				t.Fatal("invalid provisional guard was accepted")
			}
		})
	}
}

func TestRecordAuthorityGuardRequiresRedisMajorSevenAndExposesConfigDigest(t *testing.T) {
	bundle := recordAuthorityBundleForTest(t)
	for _, version := range []string{"7.0", "7.2.5", "7.2.5.1"} {
		input := bundle.guardInput
		input.RedisVersion = version
		if _, err := NewGuardCore(input); err != nil {
			t.Fatalf("Redis version %q rejected: %v", version, err)
		}
	}
	for _, version := range []string{"6.2.0", "8.0.0", "70.0", "07.0", "7", "7.x", "7..1"} {
		input := bundle.guardInput
		input.RedisVersion = version
		if _, err := NewGuardCore(input); !errors.Is(err, ErrInvalidRecordValue) {
			t.Fatalf("Redis version %q error = %v", version, err)
		}
	}
	if got := bundle.guard.RedisConfigSHA256(); got != bundle.guardInput.RedisConfigSHA256 {
		t.Fatal("guard Redis config getter mismatch")
	}
	if got := bundle.compatibility.RedisConfigSHA256(); got != bundle.compatibilityInput.RedisConfigSHA256 {
		t.Fatal("compatibility Redis config getter mismatch")
	}
	if bundle.guard.Cutover() != CutoverFresh || bundle.guard.CandidateRun() != "" {
		t.Fatal("existing guard semantic getters changed")
	}
	if bundle.legacy.V1Count() != 0 {
		t.Fatal("existing legacy V1 count getter changed")
	}
}

func TestRecordAuthorityDurabilityUsesNormativeFieldNames(t *testing.T) {
	want := []string{
		"schema_version", "boot_state", "approved_redis_run_id", "boot_epoch", "approved_at_ms",
		"planned_shutdown_nonce", "planned_shutdown_evidence_sha256", "last_approval_mode",
		"consumed_planned_shutdown_nonce", "rehearsal_evidence_sha256", "rehearsal_at_ms",
		"acknowledged_loss_bound",
	}
	got := durabilityFieldNames()
	if len(got) != len(want) {
		t.Fatalf("durability field count = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("durability field %d = %q, want %q", index, got[index], want[index])
		}
	}
}

type recordAuthorityBundle struct {
	guardInput         GuardCoreInput
	guard              GuardCore
	compatibilityInput CompatibilityArtifactInput
	compatibility      CompatibilityArtifact
	marker             CompatibilityMarker
	storedGuard        StoredCommitGuard
	legacyInput        LegacyRetirementRecordInput
	legacy             LegacyRetirementRecord
	adminInput         AdminFreezeRecordInput
	admin              AdminFreezeRecord
	durabilityInput    DurabilityRecordInput
	durability         DurabilityRecord
	firstRequest       FirstRequestStartEvidence
}

func recordAuthorityBundleForTest(t *testing.T) recordAuthorityBundle {
	t.Helper()
	digestA := Digest(strings.Repeat("a", 64))
	digestB := Digest(strings.Repeat("b", 64))
	digestC := Digest(strings.Repeat("c", 64))
	digestD := Digest(strings.Repeat("d", 64))
	digestE := Digest(strings.Repeat("e", 64))
	digestF := Digest(strings.Repeat("f", 64))
	guardInput := GuardCoreInput{
		ContractSHA256: digestA, RedisVersion: "7.2.5", RedisConfigSHA256: digestB,
		MaximumShapeSHA256: digestC, MemoryFixtureSHA256: digestD, LuaBenchmarkSHA256: digestE,
		AOFCrashEvidenceSHA256: digestF, CutoverMode: CutoverFresh,
	}
	guard, err := NewGuardCore(guardInput)
	if err != nil {
		t.Fatal(err)
	}
	guardDigest, err := guard.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	image := ImageDigest("sha256:" + strings.Repeat("1", 64))
	compatibilityInput := CompatibilityArtifactInput{
		RedisConfigSHA256: digestB, CommitGuardSHA256: guardDigest,
		SpiderImage: image, SeedImporterImage: image, CrawlAdminImage: image, IndexerImage: image,
		ImageIndexerImage: image, BacklinksProcessorImage: image, MonitoringImage: image, RenderWorkerImage: "disabled",
	}
	compatibility, err := NewCompatibilityArtifact(compatibilityInput)
	if err != nil {
		t.Fatal(err)
	}
	marker, err := NewCompatibilityMarker(compatibility)
	if err != nil {
		t.Fatal(err)
	}
	manifestDigest, err := marker.ManifestSHA256()
	if err != nil {
		t.Fatal(err)
	}
	storedGuard, err := NewStoredCommitGuard(guard, manifestDigest, 300)
	if err != nil {
		t.Fatal(err)
	}
	legacyInput := LegacyRetirementRecordInput{
		FreezeNonce: strings.Repeat("2", 32), BackupSHA256: digestA, V1SourceSHA256: digestB,
		V1QueueEvidenceSHA256: digestC, V1URLsEvidenceSHA256: digestD, V1DepthsEvidenceSHA256: digestE,
		SpiderQueueType: LegacyTypeNone, SpiderQueueEvidenceSHA256: digestF,
		SignalQueueType: LegacyTypeNone, SignalQueueEvidenceSHA256: digestA,
		DeletedBitmap: "00000", RetiredAtMS: 400,
	}
	legacy, err := NewLegacyRetirementRecord(legacyInput)
	if err != nil {
		t.Fatal(err)
	}
	adminInput := AdminFreezeRecordInput{
		FreezeNonce: strings.Repeat("2", 32), ProcessStopEvidenceSHA256: digestA,
		CandidateManifestSHA256: manifestDigest, CandidateContractSHA256: digestA, CreatedAtMS: 500,
	}
	admin, err := NewAdminFreezeRecord(adminInput)
	if err != nil {
		t.Fatal(err)
	}
	durabilityInput := DurabilityRecordInput{
		BootState: BootApproved, ApprovedRedisRunID: strings.Repeat("3", 40), BootEpoch: strings.Repeat("4", 32),
		ApprovedAtMS: 600, LastApprovalMode: ApprovalInitial, RehearsalEvidenceSHA256: digestA, RehearsalAtMS: 550,
	}
	durability, err := NewDurabilityRecord(durabilityInput)
	if err != nil {
		t.Fatal(err)
	}
	firstRequest, err := NewFirstRequestStartEvidence(RunID(strings.Repeat("5", 32)), JobID(strings.Repeat("6", 64)), Fence(1), 700)
	if err != nil {
		t.Fatal(err)
	}
	return recordAuthorityBundle{
		guardInput: guardInput, guard: guard, compatibilityInput: compatibilityInput, compatibility: compatibility,
		marker: marker, storedGuard: storedGuard, legacyInput: legacyInput, legacy: legacy,
		adminInput: adminInput, admin: admin, durabilityInput: durabilityInput, durability: durability, firstRequest: firstRequest,
	}
}

func recordAuthorityCodecCases(t *testing.T) []recordAuthorityCodecCase {
	t.Helper()
	bundle := recordAuthorityBundleForTest(t)
	compatibilityRecord, _ := bundle.compatibility.Record()
	compatibilityEncoded, _ := bundle.compatibility.Encode()
	markerRecord, _ := bundle.marker.Record()
	markerEncoded, _ := bundle.marker.Encode()
	guardRecord, _ := bundle.guard.Record()
	guardEncoded, _ := bundle.guard.Encode()
	storedGuardRecord, _ := bundle.storedGuard.Record()
	storedGuardEncoded, _ := bundle.storedGuard.Encode()
	legacyRecord, _ := bundle.legacy.Record()
	legacyEncoded, _ := bundle.legacy.Encode()
	adminRecord, _ := bundle.admin.Record()
	adminEncoded, _ := bundle.admin.Encode()
	durabilityRecord, _ := bundle.durability.Record()
	durabilityEncoded, _ := bundle.durability.Encode()
	firstRequestRecord, _ := bundle.firstRequest.Record()
	firstRequestEncoded, _ := bundle.firstRequest.Encode()

	return []recordAuthorityCodecCase{
		{
			name: "compatibility artifact", schema: SchemaCompatibilityArtifact, record: compatibilityRecord, encoded: compatibilityEncoded,
			roundTrip: func(encoded []byte) ([]byte, error) {
				decoded, err := DecodeCompatibilityArtifact(encoded)
				if err != nil {
					return nil, err
				}
				return decoded.Encode()
			},
		},
		{
			name: "compatibility marker", schema: SchemaCompatibilityMarker, record: markerRecord, encoded: markerEncoded,
			roundTrip: func(encoded []byte) ([]byte, error) {
				decoded, err := DecodeCompatibilityMarker(encoded)
				if err != nil {
					return nil, err
				}
				return decoded.Encode()
			},
		},
		{
			name: "guard core", schema: SchemaGuardCore, record: guardRecord, encoded: guardEncoded,
			roundTrip: func(encoded []byte) ([]byte, error) {
				decoded, err := DecodeGuardCore(encoded)
				if err != nil {
					return nil, err
				}
				return decoded.Encode()
			},
		},
		{
			name: "stored commit guard", schema: SchemaCommitGuard, record: storedGuardRecord, encoded: storedGuardEncoded,
			roundTrip: func(encoded []byte) ([]byte, error) {
				decoded, err := DecodeStoredCommitGuard(encoded)
				if err != nil {
					return nil, err
				}
				return decoded.Encode()
			},
		},
		{
			name: "legacy retirement", schema: SchemaLegacyRetirement, record: legacyRecord, encoded: legacyEncoded,
			roundTrip: func(encoded []byte) ([]byte, error) {
				decoded, err := DecodeLegacyRetirementRecord(encoded)
				if err != nil {
					return nil, err
				}
				return decoded.Encode()
			},
		},
		{
			name: "admin freeze", schema: SchemaAdminFreeze, record: adminRecord, encoded: adminEncoded,
			roundTrip: func(encoded []byte) ([]byte, error) {
				decoded, err := DecodeAdminFreezeRecord(encoded)
				if err != nil {
					return nil, err
				}
				return decoded.Encode()
			},
		},
		{
			name: "durability", schema: SchemaDurability, record: durabilityRecord, encoded: durabilityEncoded,
			roundTrip: func(encoded []byte) ([]byte, error) {
				decoded, err := DecodeDurabilityRecord(encoded)
				if err != nil {
					return nil, err
				}
				return decoded.Encode()
			},
		},
		{
			name: "first request start", schema: SchemaFirstRequestStart, record: firstRequestRecord, encoded: firstRequestEncoded,
			roundTrip: func(encoded []byte) ([]byte, error) {
				decoded, err := DecodeFirstRequestStartEvidence(encoded)
				if err != nil {
					return nil, err
				}
				return decoded.Encode()
			},
		},
	}
}
