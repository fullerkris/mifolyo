package crawljobsv2

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalidRecordEncoding = errors.New("crawljobsv2: invalid RECORD encoding")
	ErrRecordEncodingBounds  = errors.New("crawljobsv2: RECORD encoding bounds exceeded")
	ErrInvalidRecordValue    = errors.New("crawljobsv2: invalid fixed-record value")
	ErrArtifactMismatch      = errors.New("crawljobsv2: authority artifact mismatch")
)

const (
	maxSmallAuthorityRecordBytes = 16 * 1024
	maxRedisVersionBytes         = 64
	maxFinalImageRecordBytes     = 8 * 1024
	maxImageManifestRecordBytes  = MaxImageManifestBytes + 8*1024
	maxFinalPageRecordBytes      = MaxCombinedHTMLBytes + 16*1024
	ZeroSHA256                   = "0000000000000000000000000000000000000000000000000000000000000000"
)

type ImageDigest string

func ParseImageDigest(value string) (ImageDigest, error) {
	if !strings.HasPrefix(value, "sha256:") || !isLowerHex(strings.TrimPrefix(value, "sha256:"), 64) {
		return "", ErrInvalidRecordValue
	}
	return ImageDigest(value), nil
}

type CompatibilityArtifactInput struct {
	RedisConfigSHA256       Digest
	CommitGuardSHA256       Digest
	SpiderImage             ImageDigest
	SeedImporterImage       ImageDigest
	CrawlAdminImage         ImageDigest
	IndexerImage            ImageDigest
	ImageIndexerImage       ImageDigest
	BacklinksProcessorImage ImageDigest
	MonitoringImage         ImageDigest
	RenderWorkerImage       string
}

// CompatibilityArtifact is the reviewed artifact shape. The fixed protocol
// constants are deliberately not caller-settable.
type CompatibilityArtifact struct {
	input       CompatibilityArtifactInput
	initialized bool
}

func NewCompatibilityArtifact(input CompatibilityArtifactInput) (CompatibilityArtifact, error) {
	artifact := CompatibilityArtifact{input: input, initialized: true}
	if err := artifact.validate(); err != nil {
		return CompatibilityArtifact{}, err
	}
	return artifact, nil
}

func (artifact CompatibilityArtifact) validate() error {
	if !artifact.initialized {
		return ErrInvalidRecordValue
	}
	if err := validateDigest(artifact.input.RedisConfigSHA256); err != nil {
		return err
	}
	if err := validateDigest(artifact.input.CommitGuardSHA256); err != nil || artifact.input.CommitGuardSHA256 == Digest(ZeroSHA256) {
		return ErrInvalidRecordValue
	}
	for _, value := range []ImageDigest{
		artifact.input.SpiderImage,
		artifact.input.SeedImporterImage,
		artifact.input.CrawlAdminImage,
		artifact.input.IndexerImage,
		artifact.input.ImageIndexerImage,
		artifact.input.BacklinksProcessorImage,
		artifact.input.MonitoringImage,
	} {
		if _, err := ParseImageDigest(string(value)); err != nil {
			return err
		}
	}
	if artifact.input.RenderWorkerImage != "disabled" {
		if _, err := ParseImageDigest(artifact.input.RenderWorkerImage); err != nil {
			return err
		}
	}
	return nil
}

func (artifact CompatibilityArtifact) Record() (Record, error) {
	if err := artifact.validate(); err != nil {
		return nil, err
	}
	return Record{
		textField("manifest_version", "1"),
		textField("crawl_jobs", "2"),
		textField("crawl_policy", "2"),
		textField("canonicalization", "1"),
		textField("page_publication", "1"),
		textField("image_manifest", "1"),
		textField("backlink_projection", "1"),
		textField("render_ipc", "2"),
		textField("signal_queue", "retired"),
		textField("global_request_concurrency", "2"),
		textField("redis_config_sha256", string(artifact.input.RedisConfigSHA256)),
		textField("commit_guard_sha256", string(artifact.input.CommitGuardSHA256)),
		textField("spider_image", string(artifact.input.SpiderImage)),
		textField("seed_importer_image", string(artifact.input.SeedImporterImage)),
		textField("crawl_admin_image", string(artifact.input.CrawlAdminImage)),
		textField("indexer_image", string(artifact.input.IndexerImage)),
		textField("image_indexer_image", string(artifact.input.ImageIndexerImage)),
		textField("backlinks_processor_image", string(artifact.input.BacklinksProcessorImage)),
		textField("monitoring_image", string(artifact.input.MonitoringImage)),
		textField("render_worker_image", artifact.input.RenderWorkerImage),
	}, nil
}

func (artifact CompatibilityArtifact) Encode() ([]byte, error) {
	record, err := artifact.Record()
	if err != nil {
		return nil, err
	}
	return encodeBoundedRecord(record, maxSmallAuthorityRecordBytes)
}

func EncodeCompatibilityArtifact(artifact CompatibilityArtifact) ([]byte, error) {
	return artifact.Encode()
}

func DecodeCompatibilityArtifact(encoded []byte) (CompatibilityArtifact, error) {
	record, err := decodeExactRecord(encoded, compatibilityArtifactFieldNames(), maxSmallAuthorityRecordBytes)
	if err != nil {
		return CompatibilityArtifact{}, err
	}
	values, err := strictTextValues(record)
	if err != nil {
		return CompatibilityArtifact{}, err
	}
	if err := requireConstants(values, map[int]string{0: "1", 1: "2", 2: "2", 3: "1", 4: "1", 5: "1", 6: "1", 7: "2", 8: "retired", 9: "2"}); err != nil {
		return CompatibilityArtifact{}, err
	}
	redisConfig, err := ParseDigest(values[10])
	if err != nil {
		return CompatibilityArtifact{}, err
	}
	commitGuard, err := ParseDigest(values[11])
	if err != nil {
		return CompatibilityArtifact{}, err
	}
	images := make([]ImageDigest, 7)
	for index := range images {
		images[index], err = ParseImageDigest(values[12+index])
		if err != nil {
			return CompatibilityArtifact{}, err
		}
	}
	return NewCompatibilityArtifact(CompatibilityArtifactInput{
		RedisConfigSHA256: redisConfig, CommitGuardSHA256: commitGuard,
		SpiderImage: images[0], SeedImporterImage: images[1], CrawlAdminImage: images[2],
		IndexerImage: images[3], ImageIndexerImage: images[4], BacklinksProcessorImage: images[5],
		MonitoringImage: images[6], RenderWorkerImage: values[19],
	})
}

func (artifact CompatibilityArtifact) SHA256() (Digest, error) {
	encoded, err := artifact.Encode()
	if err != nil {
		return "", err
	}
	return plainSHA256(encoded), nil
}

func (artifact CompatibilityArtifact) CommitGuardDigest() Digest {
	return artifact.input.CommitGuardSHA256
}

type CompatibilityMarker struct {
	artifact       CompatibilityArtifact
	manifestSHA256 Digest
	initialized    bool
}

func NewCompatibilityMarker(artifact CompatibilityArtifact) (CompatibilityMarker, error) {
	digest, err := artifact.SHA256()
	if err != nil {
		return CompatibilityMarker{}, err
	}
	return CompatibilityMarker{artifact: artifact, manifestSHA256: digest, initialized: true}, nil
}

func (marker CompatibilityMarker) validate() error {
	if !marker.initialized {
		return ErrInvalidRecordValue
	}
	if err := marker.artifact.validate(); err != nil {
		return err
	}
	expected, err := marker.artifact.SHA256()
	if err != nil || marker.manifestSHA256 != expected {
		return ErrArtifactMismatch
	}
	return nil
}

func (marker CompatibilityMarker) Record() (Record, error) {
	if err := marker.validate(); err != nil {
		return nil, err
	}
	artifactRecord, _ := marker.artifact.Record()
	record := make(Record, 0, len(artifactRecord)+1)
	record = append(record, cloneField(artifactRecord[0]))
	record = append(record, textField("manifest_sha256", string(marker.manifestSHA256)))
	record = append(record, cloneRecord(artifactRecord[1:])...)
	return record, nil
}

func (marker CompatibilityMarker) Encode() ([]byte, error) {
	record, err := marker.Record()
	if err != nil {
		return nil, err
	}
	return encodeBoundedRecord(record, maxSmallAuthorityRecordBytes)
}

func EncodeCompatibilityMarker(marker CompatibilityMarker) ([]byte, error) { return marker.Encode() }

func DecodeCompatibilityMarker(encoded []byte) (CompatibilityMarker, error) {
	record, err := decodeExactRecord(encoded, compatibilityMarkerFieldNames(), maxSmallAuthorityRecordBytes)
	if err != nil {
		return CompatibilityMarker{}, err
	}
	values, err := strictTextValues(record)
	if err != nil {
		return CompatibilityMarker{}, err
	}
	manifestDigest, err := ParseDigest(values[1])
	if err != nil {
		return CompatibilityMarker{}, err
	}
	artifactRecord := make(Record, 0, len(record)-1)
	artifactRecord = append(artifactRecord, cloneField(record[0]))
	artifactRecord = append(artifactRecord, cloneRecord(record[2:])...)
	artifactEncoded, err := EncodeRecord(artifactRecord)
	if err != nil {
		return CompatibilityMarker{}, err
	}
	artifact, err := DecodeCompatibilityArtifact(artifactEncoded)
	if err != nil {
		return CompatibilityMarker{}, err
	}
	marker := CompatibilityMarker{artifact: artifact, manifestSHA256: manifestDigest, initialized: true}
	if err := marker.validate(); err != nil {
		return CompatibilityMarker{}, err
	}
	return marker, nil
}

func (marker CompatibilityMarker) Artifact() (CompatibilityArtifact, error) {
	if err := marker.validate(); err != nil {
		return CompatibilityArtifact{}, err
	}
	return marker.artifact, nil
}

func (marker CompatibilityMarker) ManifestSHA256() (Digest, error) {
	if err := marker.validate(); err != nil {
		return "", err
	}
	return marker.manifestSHA256, nil
}

type CutoverMode string

const (
	CutoverFresh       CutoverMode = "fresh"
	CutoverV1Migration CutoverMode = "v1_migration"
)

type GuardCoreInput struct {
	ContractSHA256         Digest
	RedisVersion           string
	RedisConfigSHA256      Digest
	MaximumShapeSHA256     Digest
	MemoryFixtureSHA256    Digest
	LuaBenchmarkSHA256     Digest
	AOFCrashEvidenceSHA256 Digest
	CutoverMode            CutoverMode
	CandidateRunID         RunID
}

type GuardCore struct {
	input       GuardCoreInput
	initialized bool
}

func NewGuardCore(input GuardCoreInput) (GuardCore, error) {
	core := GuardCore{input: input, initialized: true}
	if err := core.validate(); err != nil {
		return GuardCore{}, err
	}
	return core, nil
}

func (core GuardCore) validate() error {
	if !core.initialized || !validRedisVersion(core.input.RedisVersion) {
		return ErrInvalidRecordValue
	}
	for _, digest := range []Digest{
		core.input.ContractSHA256, core.input.RedisConfigSHA256, core.input.MaximumShapeSHA256,
		core.input.MemoryFixtureSHA256, core.input.LuaBenchmarkSHA256, core.input.AOFCrashEvidenceSHA256,
	} {
		if err := validateDigest(digest); err != nil || digest == Digest(ZeroSHA256) {
			return ErrInvalidRecordValue
		}
	}
	switch core.input.CutoverMode {
	case CutoverFresh:
		if core.input.CandidateRunID != "" {
			return ErrInvalidRecordValue
		}
	case CutoverV1Migration:
		if err := validateRunID(core.input.CandidateRunID); err != nil {
			return err
		}
	default:
		return ErrInvalidRecordValue
	}
	return nil
}

func (core GuardCore) Record() (Record, error) {
	if err := core.validate(); err != nil {
		return nil, err
	}
	return Record{
		textField("protocol_version", "2"),
		textField("contract_sha256", string(core.input.ContractSHA256)),
		textField("redis_version", core.input.RedisVersion),
		textField("redis_config_sha256", string(core.input.RedisConfigSHA256)),
		textField("maximum_shape_sha256", string(core.input.MaximumShapeSHA256)),
		textField("memory_fixture_sha256", string(core.input.MemoryFixtureSHA256)),
		textField("lua_benchmark_sha256", string(core.input.LuaBenchmarkSHA256)),
		textField("aof_crash_evidence_sha256", string(core.input.AOFCrashEvidenceSHA256)),
		textField("cutover_mode", string(core.input.CutoverMode)),
		textField("candidate_run_id", string(core.input.CandidateRunID)),
		textField("approved", "1"),
	}, nil
}

func (core GuardCore) Encode() ([]byte, error) {
	record, err := core.Record()
	if err != nil {
		return nil, err
	}
	return encodeBoundedRecord(record, maxSmallAuthorityRecordBytes)
}

func EncodeGuardCore(core GuardCore) ([]byte, error) { return core.Encode() }

func DecodeGuardCore(encoded []byte) (GuardCore, error) {
	record, err := decodeExactRecord(encoded, guardCoreFieldNames(), maxSmallAuthorityRecordBytes)
	if err != nil {
		return GuardCore{}, err
	}
	values, err := strictTextValues(record)
	if err != nil || values[0] != "2" || values[10] != "1" {
		return GuardCore{}, ErrInvalidRecordValue
	}
	input, err := parseGuardCoreValues(values)
	if err != nil {
		return GuardCore{}, err
	}
	return NewGuardCore(input)
}

func (core GuardCore) SHA256() (Digest, error) {
	encoded, err := core.Encode()
	if err != nil {
		return "", err
	}
	return plainSHA256(encoded), nil
}

func (core GuardCore) ContractSHA256() Digest { return core.input.ContractSHA256 }
func (core GuardCore) Cutover() CutoverMode   { return core.input.CutoverMode }
func (core GuardCore) CandidateRun() RunID    { return core.input.CandidateRunID }

type StoredCommitGuard struct {
	core                        GuardCore
	compatibilityManifestSHA256 Digest
	approvedAtMS                uint64
	initialized                 bool
}

func NewStoredCommitGuard(core GuardCore, compatibilityManifestSHA256 Digest, approvedAtMS uint64) (StoredCommitGuard, error) {
	guard := StoredCommitGuard{
		core: core, compatibilityManifestSHA256: compatibilityManifestSHA256,
		approvedAtMS: approvedAtMS, initialized: true,
	}
	if err := guard.validate(); err != nil {
		return StoredCommitGuard{}, err
	}
	return guard, nil
}

func (guard StoredCommitGuard) validate() error {
	if !guard.initialized {
		return ErrInvalidRecordValue
	}
	if err := guard.core.validate(); err != nil {
		return err
	}
	if err := validateDigest(guard.compatibilityManifestSHA256); err != nil {
		return err
	}
	return validatePositiveExactInteger(guard.approvedAtMS)
}

func (guard StoredCommitGuard) Record() (Record, error) {
	if err := guard.validate(); err != nil {
		return nil, err
	}
	coreRecord, _ := guard.core.Record()
	return Record{
		cloneField(coreRecord[0]), cloneField(coreRecord[1]),
		textField("compatibility_manifest_sha256", string(guard.compatibilityManifestSHA256)),
		cloneField(coreRecord[2]), cloneField(coreRecord[3]), cloneField(coreRecord[4]),
		cloneField(coreRecord[5]), cloneField(coreRecord[6]), cloneField(coreRecord[7]),
		cloneField(coreRecord[8]), cloneField(coreRecord[9]),
		textField("approved_at_ms", canonicalDecimal(guard.approvedAtMS)),
		cloneField(coreRecord[10]),
	}, nil
}

func (guard StoredCommitGuard) Encode() ([]byte, error) {
	record, err := guard.Record()
	if err != nil {
		return nil, err
	}
	return encodeBoundedRecord(record, maxSmallAuthorityRecordBytes)
}

func EncodeStoredCommitGuard(guard StoredCommitGuard) ([]byte, error) { return guard.Encode() }

func DecodeStoredCommitGuard(encoded []byte) (StoredCommitGuard, error) {
	record, err := decodeExactRecord(encoded, commitGuardFieldNames(), maxSmallAuthorityRecordBytes)
	if err != nil {
		return StoredCommitGuard{}, err
	}
	values, err := strictTextValues(record)
	if err != nil || values[0] != "2" || values[12] != "1" {
		return StoredCommitGuard{}, ErrInvalidRecordValue
	}
	coreValues := []string{values[0], values[1], values[3], values[4], values[5], values[6], values[7], values[8], values[9], values[10], values[12]}
	input, err := parseGuardCoreValues(coreValues)
	if err != nil {
		return StoredCommitGuard{}, err
	}
	core, err := NewGuardCore(input)
	if err != nil {
		return StoredCommitGuard{}, err
	}
	manifest, err := ParseDigest(values[2])
	if err != nil {
		return StoredCommitGuard{}, err
	}
	approvedAt, err := parsePositiveDecimal(values[11])
	if err != nil {
		return StoredCommitGuard{}, err
	}
	return NewStoredCommitGuard(core, manifest, approvedAt)
}

func (guard StoredCommitGuard) GuardCore() (GuardCore, error) {
	if err := guard.validate(); err != nil {
		return GuardCore{}, err
	}
	return guard.core, nil
}

func (guard StoredCommitGuard) CompatibilityManifestSHA256() (Digest, error) {
	if err := guard.validate(); err != nil {
		return "", err
	}
	return guard.compatibilityManifestSHA256, nil
}

type LegacyRedisType string

const (
	LegacyTypeNone LegacyRedisType = "none"
	LegacyTypeList LegacyRedisType = "list"
	LegacyTypeZSet LegacyRedisType = "zset"
)

type LegacyRetirementRecordInput struct {
	FreezeNonce               string
	BackupSHA256              Digest
	V1Count                   uint64
	V1URLFieldCount           uint64
	V1DepthFieldCount         uint64
	V1SourceSHA256            Digest
	V1QueueEvidenceSHA256     Digest
	V1URLsEvidenceSHA256      Digest
	V1DepthsEvidenceSHA256    Digest
	SpiderQueueType           LegacyRedisType
	SpiderQueueCount          uint64
	SpiderQueueEvidenceSHA256 Digest
	SignalQueueType           LegacyRedisType
	SignalQueueCount          uint64
	SignalQueueEvidenceSHA256 Digest
	DeletedBitmap             string
	RetiredAtMS               uint64
}

type LegacyRetirementRecord struct {
	input       LegacyRetirementRecordInput
	initialized bool
}

func NewLegacyRetirementRecord(input LegacyRetirementRecordInput) (LegacyRetirementRecord, error) {
	record := LegacyRetirementRecord{input: input, initialized: true}
	if err := record.validate(); err != nil {
		return LegacyRetirementRecord{}, err
	}
	return record, nil
}

func (record LegacyRetirementRecord) validate() error {
	if !record.initialized || !isLowerHex(record.input.FreezeNonce, 32) {
		return ErrInvalidRecordValue
	}
	for _, digest := range []Digest{
		record.input.BackupSHA256, record.input.V1SourceSHA256, record.input.V1QueueEvidenceSHA256,
		record.input.V1URLsEvidenceSHA256, record.input.V1DepthsEvidenceSHA256,
		record.input.SpiderQueueEvidenceSHA256, record.input.SignalQueueEvidenceSHA256,
	} {
		if err := validateDigest(digest); err != nil {
			return err
		}
	}
	if record.input.V1Count > MaxJobsPerRun || record.input.V1URLFieldCount > 2*MaxJobsPerRun || record.input.V1DepthFieldCount > 2*MaxJobsPerRun ||
		record.input.V1URLFieldCount < record.input.V1Count || record.input.V1DepthFieldCount < record.input.V1Count {
		return ErrInvalidRecordValue
	}
	if !validLegacyTypeAndCount(record.input.SpiderQueueType, record.input.SpiderQueueCount, true) ||
		!validLegacyTypeAndCount(record.input.SignalQueueType, record.input.SignalQueueCount, false) {
		return ErrInvalidRecordValue
	}
	if len(record.input.DeletedBitmap) != 5 {
		return ErrInvalidRecordValue
	}
	for _, bit := range []struct {
		index   int
		present bool
	}{
		{0, record.input.V1Count > 0},
		{1, record.input.V1URLFieldCount > 0},
		{2, record.input.V1DepthFieldCount > 0},
		{3, record.input.SpiderQueueType != LegacyTypeNone},
		{4, record.input.SignalQueueType != LegacyTypeNone},
	} {
		expected := byte('0')
		if bit.present {
			expected = '1'
		}
		if record.input.DeletedBitmap[bit.index] != expected {
			return ErrInvalidRecordValue
		}
	}
	return validatePositiveExactInteger(record.input.RetiredAtMS)
}

func (record LegacyRetirementRecord) Record() (Record, error) {
	if err := record.validate(); err != nil {
		return nil, err
	}
	input := record.input
	return Record{
		textField("protocol_version", "2"), textField("freeze_nonce", input.FreezeNonce),
		textField("backup_sha256", string(input.BackupSHA256)), textField("v1_count", canonicalDecimal(input.V1Count)),
		textField("v1_url_field_count", canonicalDecimal(input.V1URLFieldCount)), textField("v1_depth_field_count", canonicalDecimal(input.V1DepthFieldCount)),
		textField("v1_source_sha256", string(input.V1SourceSHA256)), textField("v1_queue_evidence_sha256", string(input.V1QueueEvidenceSHA256)),
		textField("v1_urls_evidence_sha256", string(input.V1URLsEvidenceSHA256)), textField("v1_depths_evidence_sha256", string(input.V1DepthsEvidenceSHA256)),
		textField("spider_queue_type", string(input.SpiderQueueType)), textField("spider_queue_count", canonicalDecimal(input.SpiderQueueCount)),
		textField("spider_queue_evidence_sha256", string(input.SpiderQueueEvidenceSHA256)), textField("signal_queue_type", string(input.SignalQueueType)),
		textField("signal_queue_count", canonicalDecimal(input.SignalQueueCount)), textField("signal_queue_evidence_sha256", string(input.SignalQueueEvidenceSHA256)),
		textField("deleted_bitmap", input.DeletedBitmap), textField("retired_at_ms", canonicalDecimal(input.RetiredAtMS)),
	}, nil
}

func (record LegacyRetirementRecord) Encode() ([]byte, error) {
	fields, err := record.Record()
	if err != nil {
		return nil, err
	}
	return encodeBoundedRecord(fields, maxSmallAuthorityRecordBytes)
}

func EncodeLegacyRetirementRecord(record LegacyRetirementRecord) ([]byte, error) {
	return record.Encode()
}

func DecodeLegacyRetirementRecord(encoded []byte) (LegacyRetirementRecord, error) {
	record, err := decodeExactRecord(encoded, legacyRetirementFieldNames(), maxSmallAuthorityRecordBytes)
	if err != nil {
		return LegacyRetirementRecord{}, err
	}
	values, err := strictTextValues(record)
	if err != nil || values[0] != "2" {
		return LegacyRetirementRecord{}, ErrInvalidRecordValue
	}
	digests := make(map[int]Digest, 7)
	for _, index := range []int{2, 6, 7, 8, 9, 12, 15} {
		digests[index], err = ParseDigest(values[index])
		if err != nil {
			return LegacyRetirementRecord{}, err
		}
	}
	counts := make(map[int]uint64, 6)
	for _, index := range []int{3, 4, 5, 11, 14, 17} {
		counts[index], err = parseCanonicalDecimal(values[index])
		if err != nil {
			return LegacyRetirementRecord{}, err
		}
	}
	return NewLegacyRetirementRecord(LegacyRetirementRecordInput{
		FreezeNonce: values[1], BackupSHA256: digests[2], V1Count: counts[3], V1URLFieldCount: counts[4],
		V1DepthFieldCount: counts[5], V1SourceSHA256: digests[6], V1QueueEvidenceSHA256: digests[7],
		V1URLsEvidenceSHA256: digests[8], V1DepthsEvidenceSHA256: digests[9], SpiderQueueType: LegacyRedisType(values[10]),
		SpiderQueueCount: counts[11], SpiderQueueEvidenceSHA256: digests[12], SignalQueueType: LegacyRedisType(values[13]),
		SignalQueueCount: counts[14], SignalQueueEvidenceSHA256: digests[15], DeletedBitmap: values[16], RetiredAtMS: counts[17],
	})
}

func (record LegacyRetirementRecord) FreezeNonce() string { return record.input.FreezeNonce }
func (record LegacyRetirementRecord) V1Count() uint64     { return record.input.V1Count }

type AdminFreezeRecordInput struct {
	FreezeNonce               string
	ProcessStopEvidenceSHA256 Digest
	CandidateManifestSHA256   Digest
	CandidateContractSHA256   Digest
	CreatedAtMS               uint64
}

type AdminFreezeRecord struct {
	input       AdminFreezeRecordInput
	initialized bool
}

func NewAdminFreezeRecord(input AdminFreezeRecordInput) (AdminFreezeRecord, error) {
	record := AdminFreezeRecord{input: input, initialized: true}
	if err := record.validate(); err != nil {
		return AdminFreezeRecord{}, err
	}
	return record, nil
}

func (record AdminFreezeRecord) validate() error {
	if !record.initialized || !isLowerHex(record.input.FreezeNonce, 32) {
		return ErrInvalidRecordValue
	}
	for _, digest := range []Digest{record.input.ProcessStopEvidenceSHA256, record.input.CandidateManifestSHA256, record.input.CandidateContractSHA256} {
		if err := validateDigest(digest); err != nil {
			return err
		}
	}
	return validatePositiveExactInteger(record.input.CreatedAtMS)
}

func (record AdminFreezeRecord) Record() (Record, error) {
	if err := record.validate(); err != nil {
		return nil, err
	}
	return Record{
		textField("protocol_version", "2"), textField("freeze_nonce", record.input.FreezeNonce),
		textField("process_stop_evidence_sha256", string(record.input.ProcessStopEvidenceSHA256)),
		textField("candidate_manifest_sha256", string(record.input.CandidateManifestSHA256)),
		textField("candidate_contract_sha256", string(record.input.CandidateContractSHA256)),
		textField("created_at_ms", canonicalDecimal(record.input.CreatedAtMS)),
	}, nil
}

func (record AdminFreezeRecord) Encode() ([]byte, error) {
	fields, err := record.Record()
	if err != nil {
		return nil, err
	}
	return encodeBoundedRecord(fields, maxSmallAuthorityRecordBytes)
}

func EncodeAdminFreezeRecord(record AdminFreezeRecord) ([]byte, error) { return record.Encode() }

func DecodeAdminFreezeRecord(encoded []byte) (AdminFreezeRecord, error) {
	record, err := decodeExactRecord(encoded, adminFreezeFieldNames(), maxSmallAuthorityRecordBytes)
	if err != nil {
		return AdminFreezeRecord{}, err
	}
	values, err := strictTextValues(record)
	if err != nil || values[0] != "2" {
		return AdminFreezeRecord{}, ErrInvalidRecordValue
	}
	process, err := ParseDigest(values[2])
	if err != nil {
		return AdminFreezeRecord{}, err
	}
	manifest, err := ParseDigest(values[3])
	if err != nil {
		return AdminFreezeRecord{}, err
	}
	contract, err := ParseDigest(values[4])
	if err != nil {
		return AdminFreezeRecord{}, err
	}
	createdAt, err := parsePositiveDecimal(values[5])
	if err != nil {
		return AdminFreezeRecord{}, err
	}
	return NewAdminFreezeRecord(AdminFreezeRecordInput{
		FreezeNonce: values[1], ProcessStopEvidenceSHA256: process, CandidateManifestSHA256: manifest,
		CandidateContractSHA256: contract, CreatedAtMS: createdAt,
	})
}

func (record AdminFreezeRecord) FreezeNonce() string { return record.input.FreezeNonce }

type BootState string
type ApprovalMode string

const (
	BootApproved   BootState = "approved"
	BootPlanned    BootState = "planned"
	BootUnapproved BootState = "unapproved"

	ApprovalInitial          ApprovalMode = "initial"
	ApprovalPlanned          ApprovalMode = "planned"
	ApprovalUncleanRehearsal ApprovalMode = "unclean_rehearsal"
)

type DurabilityRecordInput struct {
	BootState                     BootState
	ApprovedRedisRunID            string
	BootEpoch                     string
	ApprovedAtMS                  uint64
	PlannedShutdownNonce          string
	PlannedShutdownEvidenceSHA256 Digest
	LastApprovalMode              ApprovalMode
	ConsumedPlannedShutdownNonce  string
	RehearsalEvidenceSHA256       Digest
	RehearsalAtMS                 uint64
}

type DurabilityRecord struct {
	input       DurabilityRecordInput
	initialized bool
}

func NewDurabilityRecord(input DurabilityRecordInput) (DurabilityRecord, error) {
	record := DurabilityRecord{input: input, initialized: true}
	if err := record.validate(); err != nil {
		return DurabilityRecord{}, err
	}
	return record, nil
}

func (record DurabilityRecord) validate() error {
	input := record.input
	if !record.initialized || !isLowerHex(input.ApprovedRedisRunID, 40) || !isLowerHex(input.BootEpoch, 32) {
		return ErrInvalidRecordValue
	}
	if err := validatePositiveExactInteger(input.ApprovedAtMS); err != nil {
		return err
	}
	if err := validateDigest(input.RehearsalEvidenceSHA256); err != nil {
		return err
	}
	if err := validatePositiveExactInteger(input.RehearsalAtMS); err != nil {
		return err
	}
	switch input.LastApprovalMode {
	case ApprovalInitial, ApprovalPlanned, ApprovalUncleanRehearsal:
	default:
		return ErrInvalidRecordValue
	}
	if input.PlannedShutdownNonce != "" && !isLowerHex(input.PlannedShutdownNonce, 32) ||
		input.ConsumedPlannedShutdownNonce != "" && !isLowerHex(input.ConsumedPlannedShutdownNonce, 32) ||
		input.PlannedShutdownEvidenceSHA256 != "" && validateDigest(input.PlannedShutdownEvidenceSHA256) != nil {
		return ErrInvalidRecordValue
	}
	switch input.BootState {
	case BootPlanned:
		if input.PlannedShutdownNonce == "" || input.PlannedShutdownEvidenceSHA256 == "" || input.ConsumedPlannedShutdownNonce != "" {
			return ErrInvalidRecordValue
		}
	case BootApproved:
		switch input.LastApprovalMode {
		case ApprovalPlanned:
			if input.PlannedShutdownNonce != "" || input.PlannedShutdownEvidenceSHA256 == "" || input.ConsumedPlannedShutdownNonce == "" {
				return ErrInvalidRecordValue
			}
		case ApprovalInitial, ApprovalUncleanRehearsal:
			if input.PlannedShutdownNonce != "" || input.PlannedShutdownEvidenceSHA256 != "" || input.ConsumedPlannedShutdownNonce != "" {
				return ErrInvalidRecordValue
			}
		}
	case BootUnapproved:
		if input.PlannedShutdownNonce != "" || input.PlannedShutdownEvidenceSHA256 != "" || input.ConsumedPlannedShutdownNonce != "" {
			return ErrInvalidRecordValue
		}
	default:
		return ErrInvalidRecordValue
	}
	return nil
}

func (record DurabilityRecord) Record() (Record, error) {
	if err := record.validate(); err != nil {
		return nil, err
	}
	input := record.input
	return Record{
		textField("schema_version", "1"), textField("boot_state", string(input.BootState)),
		textField("approved_redis_run_id", input.ApprovedRedisRunID), textField("boot_epoch", input.BootEpoch),
		textField("approved_at_ms", canonicalDecimal(input.ApprovedAtMS)), textField("planned_shutdown_nonce", input.PlannedShutdownNonce),
		textField("planned_shutdown_evidence_sha256", string(input.PlannedShutdownEvidenceSHA256)), textField("last_approval_mode", string(input.LastApprovalMode)),
		textField("consumed_planned_shutdown_nonce", input.ConsumedPlannedShutdownNonce), textField("rehearsal_evidence_sha256", string(input.RehearsalEvidenceSHA256)),
		textField("rehearsal_at_ms", canonicalDecimal(input.RehearsalAtMS)), textField("acknowledged_loss_bound", "0"),
	}, nil
}

func (record DurabilityRecord) Encode() ([]byte, error) {
	fields, err := record.Record()
	if err != nil {
		return nil, err
	}
	return encodeBoundedRecord(fields, maxSmallAuthorityRecordBytes)
}

func EncodeDurabilityRecord(record DurabilityRecord) ([]byte, error) { return record.Encode() }

func DecodeDurabilityRecord(encoded []byte) (DurabilityRecord, error) {
	record, err := decodeExactRecord(encoded, durabilityFieldNames(), maxSmallAuthorityRecordBytes)
	if err != nil {
		return DurabilityRecord{}, err
	}
	values, err := strictTextValues(record)
	if err != nil || values[0] != "1" || values[11] != "0" {
		return DurabilityRecord{}, ErrInvalidRecordValue
	}
	approvedAt, err := parsePositiveDecimal(values[4])
	if err != nil {
		return DurabilityRecord{}, err
	}
	rehearsalAt, err := parsePositiveDecimal(values[10])
	if err != nil {
		return DurabilityRecord{}, err
	}
	return NewDurabilityRecord(DurabilityRecordInput{
		BootState: BootState(values[1]), ApprovedRedisRunID: values[2], BootEpoch: values[3], ApprovedAtMS: approvedAt,
		PlannedShutdownNonce: values[5], PlannedShutdownEvidenceSHA256: Digest(values[6]), LastApprovalMode: ApprovalMode(values[7]),
		ConsumedPlannedShutdownNonce: values[8], RehearsalEvidenceSHA256: Digest(values[9]), RehearsalAtMS: rehearsalAt,
	})
}

type FirstRequestStartEvidence struct {
	runID       RunID
	jobID       JobID
	leaseFence  Fence
	startedAtMS uint64
	initialized bool
}

func NewFirstRequestStartEvidence(runID RunID, jobID JobID, leaseFence Fence, startedAtMS uint64) (FirstRequestStartEvidence, error) {
	evidence := FirstRequestStartEvidence{runID: runID, jobID: jobID, leaseFence: leaseFence, startedAtMS: startedAtMS, initialized: true}
	if err := evidence.validate(); err != nil {
		return FirstRequestStartEvidence{}, err
	}
	return evidence, nil
}

func (evidence FirstRequestStartEvidence) validate() error {
	if !evidence.initialized {
		return ErrInvalidRecordValue
	}
	if err := validateRunID(evidence.runID); err != nil {
		return err
	}
	if err := validateJobID(evidence.jobID); err != nil {
		return err
	}
	if _, err := evidence.leaseFence.Decimal(); err != nil {
		return err
	}
	return validatePositiveExactInteger(evidence.startedAtMS)
}

func (evidence FirstRequestStartEvidence) Record() (Record, error) {
	if err := evidence.validate(); err != nil {
		return nil, err
	}
	return Record{
		textField("protocol_version", "2"), textField("run_id", string(evidence.runID)), textField("job_id", string(evidence.jobID)),
		textField("lease_fence", canonicalDecimal(uint64(evidence.leaseFence))), textField("started_at_ms", canonicalDecimal(evidence.startedAtMS)),
	}, nil
}

func (evidence FirstRequestStartEvidence) Encode() ([]byte, error) {
	record, err := evidence.Record()
	if err != nil {
		return nil, err
	}
	return encodeBoundedRecord(record, maxSmallAuthorityRecordBytes)
}

func EncodeFirstRequestStartEvidence(evidence FirstRequestStartEvidence) ([]byte, error) {
	return evidence.Encode()
}

func DecodeFirstRequestStartEvidence(encoded []byte) (FirstRequestStartEvidence, error) {
	record, err := decodeExactRecord(encoded, firstRequestStartFieldNames(), maxSmallAuthorityRecordBytes)
	if err != nil {
		return FirstRequestStartEvidence{}, err
	}
	values, err := strictTextValues(record)
	if err != nil || values[0] != "2" {
		return FirstRequestStartEvidence{}, ErrInvalidRecordValue
	}
	runID, err := ParseRunID(values[1])
	if err != nil {
		return FirstRequestStartEvidence{}, err
	}
	jobID, err := ParseJobID(values[2])
	if err != nil {
		return FirstRequestStartEvidence{}, err
	}
	fence, err := ParseFence(values[3])
	if err != nil {
		return FirstRequestStartEvidence{}, err
	}
	startedAt, err := parsePositiveDecimal(values[4])
	if err != nil {
		return FirstRequestStartEvidence{}, err
	}
	return NewFirstRequestStartEvidence(runID, jobID, fence, startedAt)
}

// FinalPageRecord is the complete downstream ten-field page hash. Construction
// from OutputContext binds URL and last_crawled to Redis-recorded request-start
// evidence rather than caller wall time.
type FinalPageRecord struct {
	record      Record
	initialized bool
}

func NewFinalPageRecord(context OutputContext, page OutputPage, publicationID Digest) (FinalPageRecord, error) {
	if err := validateDigest(publicationID); err != nil {
		return FinalPageRecord{}, err
	}
	fields, err := outputPageRecord(context, page)
	if err != nil {
		return FinalPageRecord{}, err
	}
	fields = append(fields, textField("publication_id", string(publicationID)))
	return newFinalPageRecord(fields)
}

func newFinalPageRecord(fields Record) (FinalPageRecord, error) {
	if !hasFieldNames(fields, finalPageFieldNames()...) {
		return FinalPageRecord{}, ErrInvalidRecordValue
	}
	values, err := strictTextValues(fields)
	if err != nil {
		return FinalPageRecord{}, err
	}
	if _, err := requireCanonicalURL(values[0]); err != nil {
		return FinalPageRecord{}, err
	}
	if len(fields[1].Value) > MaxPageBlobBytes || len(fields[2].Value) > MaxPageBlobBytes || len(fields[1].Value)+len(fields[2].Value) > MaxCombinedHTMLBytes {
		return FinalPageRecord{}, ErrInvalidRecordValue
	}
	if err := validateContentType(values[3]); err != nil {
		return FinalPageRecord{}, err
	}
	if len(values[4]) != 3 || values[4][0] < '1' || values[4][0] > '3' || values[4][1] < '0' || values[4][1] > '9' || values[4][2] < '0' || values[4][2] > '9' {
		return FinalPageRecord{}, ErrInvalidRecordValue
	}
	parsedTime, err := time.Parse(lastCrawledLayout, values[5])
	if err != nil || parsedTime.UTC().Format(lastCrawledLayout) != values[5] {
		return FinalPageRecord{}, ErrInvalidRecordValue
	}
	if values[6] == "false" {
		if len(fields[2].Value) != 0 || values[7] != "" || values[8] != "" {
			return FinalPageRecord{}, ErrInvalidRecordValue
		}
	} else if values[6] == "true" {
		if len(fields[2].Value) == 0 || len(values[7]) == 0 || len(values[7]) > MaxRenderPolicyRuleIDBytes || containsControl(values[7]) {
			return FinalPageRecord{}, ErrInvalidRecordValue
		}
		if _, err := ParseDigest(values[8]); err != nil {
			return FinalPageRecord{}, err
		}
	} else {
		return FinalPageRecord{}, ErrInvalidRecordValue
	}
	if _, err := ParseDigest(values[9]); err != nil {
		return FinalPageRecord{}, err
	}
	return FinalPageRecord{record: cloneRecord(fields), initialized: true}, nil
}

func DecodeFinalPageRecord(encoded []byte) (FinalPageRecord, error) {
	record, err := decodeExactRecord(encoded, finalPageFieldNames(), maxFinalPageRecordBytes)
	if err != nil {
		return FinalPageRecord{}, err
	}
	return newFinalPageRecord(record)
}

func (record FinalPageRecord) Record() (Record, error) {
	if !record.initialized {
		return nil, ErrInvalidRecordValue
	}
	if _, err := newFinalPageRecord(record.record); err != nil {
		return nil, err
	}
	return cloneRecord(record.record), nil
}

func (record FinalPageRecord) Encode() ([]byte, error) {
	fields, err := record.Record()
	if err != nil {
		return nil, err
	}
	return encodeBoundedRecord(fields, maxFinalPageRecordBytes)
}

func EncodeFinalPageRecord(record FinalPageRecord) ([]byte, error) { return record.Encode() }

func (record FinalPageRecord) ValidateAgainstContext(context OutputContext) error {
	if !context.initialized || !record.initialized {
		return ErrArtifactMismatch
	}
	if string(record.record[0].Value) != context.finalTarget.CanonicalURL || string(record.record[5].Value) != context.lastCrawled {
		return ErrArtifactMismatch
	}
	return nil
}

type FinalImageRecord struct {
	publicationID       Digest
	normalizedPageURL   string
	normalizedSourceURL string
	alt                 string
	initialized         bool
}

func NewFinalImageRecord(publicationID Digest, normalizedPageURL string, image OutputImage) (FinalImageRecord, error) {
	record := FinalImageRecord{
		publicationID: publicationID, normalizedPageURL: normalizedPageURL,
		normalizedSourceURL: image.NormalizedSourceURL, alt: image.Alt, initialized: true,
	}
	if err := record.validate(); err != nil {
		return FinalImageRecord{}, err
	}
	return record, nil
}

func (record FinalImageRecord) validate() error {
	if !record.initialized {
		return ErrInvalidRecordValue
	}
	if err := validateDigest(record.publicationID); err != nil {
		return err
	}
	if _, err := requireCanonicalURL(record.normalizedPageURL); err != nil {
		return err
	}
	if _, err := requireCanonicalURL(record.normalizedSourceURL); err != nil {
		return err
	}
	if !utf8.ValidString(record.alt) || len(record.alt) > MaxImageAltBytes {
		return ErrInvalidRecordValue
	}
	return nil
}

func (record FinalImageRecord) Record() (Record, error) {
	if err := record.validate(); err != nil {
		return nil, err
	}
	return Record{
		textField("contract_version", "1"), textField("publication_id", string(record.publicationID)),
		textField("normalized_page_url", record.normalizedPageURL), textField("normalized_source_url", record.normalizedSourceURL),
		textField("alt", record.alt),
	}, nil
}

func (record FinalImageRecord) Encode() ([]byte, error) {
	fields, err := record.Record()
	if err != nil {
		return nil, err
	}
	return encodeBoundedRecord(fields, maxFinalImageRecordBytes)
}

func EncodeFinalImageRecord(record FinalImageRecord) ([]byte, error) { return record.Encode() }

func DecodeFinalImageRecord(encoded []byte) (FinalImageRecord, error) {
	record, err := decodeExactRecord(encoded, finalImageFieldNames(), maxFinalImageRecordBytes)
	if err != nil {
		return FinalImageRecord{}, err
	}
	values, err := strictTextValues(record)
	if err != nil || values[0] != "1" {
		return FinalImageRecord{}, ErrInvalidRecordValue
	}
	publicationID, err := ParseDigest(values[1])
	if err != nil {
		return FinalImageRecord{}, err
	}
	return NewFinalImageRecord(publicationID, values[2], OutputImage{NormalizedSourceURL: values[3], Alt: values[4]})
}

type ImageManifestRecord struct {
	publicationID Digest
	normalizedURL string
	imageKeys     []string
	initialized   bool
}

func NewImageManifestRecord(publicationID Digest, normalizedURL string, images []OutputImage) (ImageManifestRecord, error) {
	if err := validateDigest(publicationID); err != nil {
		return ImageManifestRecord{}, err
	}
	if _, err := requireCanonicalURL(normalizedURL); err != nil {
		return ImageManifestRecord{}, err
	}
	orderedRecords, err := outputImageRecords(images)
	if err != nil {
		return ImageManifestRecord{}, err
	}
	keys := make([]string, len(orderedRecords))
	for index, image := range orderedRecords {
		keys[index], err = ImageDataKey(publicationID, normalizedURL, string(image[0].Value))
		if err != nil {
			return ImageManifestRecord{}, err
		}
	}
	record := ImageManifestRecord{publicationID: publicationID, normalizedURL: normalizedURL, imageKeys: keys, initialized: true}
	if err := record.validate(); err != nil {
		return ImageManifestRecord{}, err
	}
	return record, nil
}

func (record ImageManifestRecord) validate() error {
	if !record.initialized || len(record.imageKeys) > MaxImagesPerPage {
		return ErrInvalidRecordValue
	}
	if err := validateDigest(record.publicationID); err != nil {
		return err
	}
	if _, err := requireCanonicalURL(record.normalizedURL); err != nil {
		return err
	}
	encodedKeys, err := json.Marshal(record.imageKeys)
	if err != nil || len(encodedKeys) > MaxImageManifestBytes {
		return ErrInvalidRecordValue
	}
	prefix := "image_data:" + string(record.publicationID) + ":" + base64.RawURLEncoding.EncodeToString([]byte(record.normalizedURL)) + ":"
	previous := ""
	for index, key := range record.imageKeys {
		if !strings.HasPrefix(key, prefix) || len(key) == len(prefix) || strings.Contains(strings.TrimPrefix(key, prefix), ":") {
			return ErrInvalidRecordValue
		}
		encodedSource := strings.TrimPrefix(key, prefix)
		source, err := base64.RawURLEncoding.DecodeString(encodedSource)
		if err != nil || base64.RawURLEncoding.EncodeToString(source) != encodedSource || !utf8.Valid(source) {
			return ErrInvalidRecordValue
		}
		if _, err := requireCanonicalURL(string(source)); err != nil {
			return err
		}
		if index > 0 && string(source) <= previous {
			return ErrInvalidRecordValue
		}
		previous = string(source)
	}
	return nil
}

func (record ImageManifestRecord) Record() (Record, error) {
	if err := record.validate(); err != nil {
		return nil, err
	}
	encodedKeys, _ := json.Marshal(record.imageKeys)
	return Record{
		textField("contract_version", "1"), textField("publication_id", string(record.publicationID)),
		textField("normalized_url", record.normalizedURL), textField("image_count", canonicalDecimal(uint64(len(record.imageKeys)))),
		{Name: "image_keys", Value: encodedKeys},
	}, nil
}

func (record ImageManifestRecord) Encode() ([]byte, error) {
	fields, err := record.Record()
	if err != nil {
		return nil, err
	}
	return encodeBoundedRecord(fields, maxImageManifestRecordBytes)
}

func EncodeImageManifestRecord(record ImageManifestRecord) ([]byte, error) { return record.Encode() }

func DecodeImageManifestRecord(encoded []byte) (ImageManifestRecord, error) {
	record, err := decodeExactRecord(encoded, imageManifestFieldNames(), maxImageManifestRecordBytes)
	if err != nil {
		return ImageManifestRecord{}, err
	}
	values, err := strictTextValues(record)
	if err != nil || values[0] != "1" || len(record[4].Value) > MaxImageManifestBytes {
		return ImageManifestRecord{}, ErrInvalidRecordValue
	}
	publicationID, err := ParseDigest(values[1])
	if err != nil {
		return ImageManifestRecord{}, err
	}
	count, err := parseCanonicalDecimal(values[3])
	if err != nil || count > MaxImagesPerPage {
		return ImageManifestRecord{}, ErrInvalidRecordValue
	}
	var keys []string
	if err := json.Unmarshal(record[4].Value, &keys); err != nil || keys == nil || uint64(len(keys)) != count {
		return ImageManifestRecord{}, ErrInvalidRecordValue
	}
	canonical, err := json.Marshal(keys)
	if err != nil || !bytes.Equal(canonical, record[4].Value) {
		return ImageManifestRecord{}, ErrInvalidRecordValue
	}
	manifest := ImageManifestRecord{publicationID: publicationID, normalizedURL: values[2], imageKeys: append([]string(nil), keys...), initialized: true}
	if err := manifest.validate(); err != nil {
		return ImageManifestRecord{}, err
	}
	return manifest, nil
}

func validateAuthorityRecord(schema RecordSchema, record Record) error {
	encoded, err := EncodeRecord(record)
	if err != nil {
		return err
	}
	switch schema {
	case SchemaCompatibilityArtifact:
		_, err = DecodeCompatibilityArtifact(encoded)
	case SchemaCompatibilityMarker:
		_, err = DecodeCompatibilityMarker(encoded)
	case SchemaGuardCore:
		_, err = DecodeGuardCore(encoded)
	case SchemaCommitGuard:
		_, err = DecodeStoredCommitGuard(encoded)
	case SchemaLegacyRetirement:
		_, err = DecodeLegacyRetirementRecord(encoded)
	case SchemaAdminFreeze:
		_, err = DecodeAdminFreezeRecord(encoded)
	case SchemaDurability:
		_, err = DecodeDurabilityRecord(encoded)
	case SchemaFirstRequestStart:
		_, err = DecodeFirstRequestStartEvidence(encoded)
	case SchemaFinalPage:
		_, err = DecodeFinalPageRecord(encoded)
	case SchemaFinalImage:
		_, err = DecodeFinalImageRecord(encoded)
	case SchemaImageManifest:
		_, err = DecodeImageManifestRecord(encoded)
	default:
		return ErrInvalidSchema
	}
	return err
}

func compatibilityArtifactFieldNames() []string {
	return []string{
		"manifest_version", "crawl_jobs", "crawl_policy", "canonicalization", "page_publication", "image_manifest",
		"backlink_projection", "render_ipc", "signal_queue", "global_request_concurrency", "redis_config_sha256",
		"commit_guard_sha256", "spider_image", "seed_importer_image", "crawl_admin_image", "indexer_image",
		"image_indexer_image", "backlinks_processor_image", "monitoring_image", "render_worker_image",
	}
}

func compatibilityMarkerFieldNames() []string {
	artifact := compatibilityArtifactFieldNames()
	fields := make([]string, 0, len(artifact)+1)
	fields = append(fields, artifact[0], "manifest_sha256")
	return append(fields, artifact[1:]...)
}

func guardCoreFieldNames() []string {
	return []string{
		"protocol_version", "contract_sha256", "redis_version", "redis_config_sha256", "maximum_shape_sha256",
		"memory_fixture_sha256", "lua_benchmark_sha256", "aof_crash_evidence_sha256", "cutover_mode", "candidate_run_id", "approved",
	}
}

func commitGuardFieldNames() []string {
	return []string{
		"protocol_version", "contract_sha256", "compatibility_manifest_sha256", "redis_version", "redis_config_sha256",
		"maximum_shape_sha256", "memory_fixture_sha256", "lua_benchmark_sha256", "aof_crash_evidence_sha256",
		"cutover_mode", "candidate_run_id", "approved_at_ms", "approved",
	}
}

func legacyRetirementFieldNames() []string {
	return []string{
		"protocol_version", "freeze_nonce", "backup_sha256", "v1_count", "v1_url_field_count", "v1_depth_field_count",
		"v1_source_sha256", "v1_queue_evidence_sha256", "v1_urls_evidence_sha256", "v1_depths_evidence_sha256",
		"spider_queue_type", "spider_queue_count", "spider_queue_evidence_sha256", "signal_queue_type", "signal_queue_count",
		"signal_queue_evidence_sha256", "deleted_bitmap", "retired_at_ms",
	}
}

func adminFreezeFieldNames() []string {
	return []string{"protocol_version", "freeze_nonce", "process_stop_evidence_sha256", "candidate_manifest_sha256", "candidate_contract_sha256", "created_at_ms"}
}

func durabilityFieldNames() []string {
	return []string{
		"schema_version", "boot_state", "approved_redis_run_id", "boot_epoch", "approved_at_ms", "planned_shutdown_nonce",
		"planned_shutdown_evidence_sha256", "last_approval_mode", "consumed_planned_shutdown_nonce", "rehearsal_evidence_sha256",
		"rehearsal_at_ms", "acknowledged_loss_bound",
	}
}

func firstRequestStartFieldNames() []string {
	return []string{"protocol_version", "run_id", "job_id", "lease_fence", "started_at_ms"}
}

func finalPageFieldNames() []string {
	return []string{
		"normalized_url", "html", "original_html", "content_type", "status_code", "last_crawled", "rendered",
		"render_policy_rule", "render_policy_sha256", "publication_id",
	}
}

func finalImageFieldNames() []string {
	return []string{"contract_version", "publication_id", "normalized_page_url", "normalized_source_url", "alt"}
}

func imageManifestFieldNames() []string {
	return []string{"contract_version", "publication_id", "normalized_url", "image_count", "image_keys"}
}

func encodeBoundedRecord(record Record, maximum int) ([]byte, error) {
	encoded, err := EncodeRecord(record)
	if err != nil {
		return nil, err
	}
	if len(encoded) > maximum {
		return nil, ErrRecordEncodingBounds
	}
	return encoded, nil
}

func decodeExactRecord(encoded []byte, names []string, maximum int) (Record, error) {
	if len(encoded) > maximum {
		return nil, ErrRecordEncodingBounds
	}
	if len(encoded) < 8 || binary.BigEndian.Uint64(encoded[:8]) != uint64(len(names)) {
		return nil, ErrInvalidRecordEncoding
	}
	offset := 8
	record := make(Record, len(names))
	for index, name := range names {
		fieldName, next, err := readBoundedFrame(encoded, offset)
		if err != nil || !bytes.Equal(fieldName, []byte(name)) {
			return nil, ErrInvalidRecordEncoding
		}
		offset = next
		value, next, err := readBoundedFrame(encoded, offset)
		if err != nil {
			return nil, err
		}
		offset = next
		record[index] = Field{Name: name, Value: append([]byte(nil), value...)}
	}
	if offset != len(encoded) {
		return nil, ErrInvalidRecordEncoding
	}
	return record, nil
}

func readBoundedFrame(encoded []byte, offset int) ([]byte, int, error) {
	if offset < 0 || len(encoded)-offset < 8 {
		return nil, 0, ErrInvalidRecordEncoding
	}
	length := binary.BigEndian.Uint64(encoded[offset : offset+8])
	offset += 8
	if length > uint64(len(encoded)-offset) {
		return nil, 0, ErrInvalidRecordEncoding
	}
	return encoded[offset : offset+int(length)], offset + int(length), nil
}

func strictTextValues(record Record) ([]string, error) {
	values := make([]string, len(record))
	for index := range record {
		if !utf8.Valid(record[index].Value) {
			return nil, ErrInvalidRecordValue
		}
		values[index] = string(record[index].Value)
	}
	return values, nil
}

func requireConstants(values []string, constants map[int]string) error {
	for index, expected := range constants {
		if index < 0 || index >= len(values) || values[index] != expected {
			return ErrInvalidRecordValue
		}
	}
	return nil
}

func plainSHA256(value []byte) Digest {
	digest := sha256.Sum256(value)
	return Digest(hex.EncodeToString(digest[:]))
}

func validRedisVersion(value string) bool {
	if value == "" || len(value) > maxRedisVersionBytes {
		return false
	}
	componentLength := 0
	dots := 0
	for index := range value {
		switch {
		case value[index] >= '0' && value[index] <= '9':
			componentLength++
		case value[index] == '.' && componentLength > 0:
			dots++
			componentLength = 0
		default:
			return false
		}
	}
	return dots >= 1 && dots <= 3 && componentLength > 0
}

func parseGuardCoreValues(values []string) (GuardCoreInput, error) {
	if len(values) != 11 || values[0] != "2" || values[10] != "1" {
		return GuardCoreInput{}, ErrInvalidRecordValue
	}
	contract, err := ParseDigest(values[1])
	if err != nil {
		return GuardCoreInput{}, err
	}
	digests := make([]Digest, 5)
	for index, value := range values[3:8] {
		digests[index], err = ParseDigest(value)
		if err != nil {
			return GuardCoreInput{}, err
		}
	}
	return GuardCoreInput{
		ContractSHA256: contract, RedisVersion: values[2], RedisConfigSHA256: digests[0], MaximumShapeSHA256: digests[1],
		MemoryFixtureSHA256: digests[2], LuaBenchmarkSHA256: digests[3], AOFCrashEvidenceSHA256: digests[4],
		CutoverMode: CutoverMode(values[8]), CandidateRunID: RunID(values[9]),
	}, nil
}

func validLegacyTypeAndCount(kind LegacyRedisType, count uint64, spider bool) bool {
	if count > MaxJobsPerRun {
		return false
	}
	switch kind {
	case LegacyTypeNone:
		return count == 0
	case LegacyTypeList:
		return count > 0
	case LegacyTypeZSet:
		return spider && count > 0
	default:
		return false
	}
}

func validatePositiveExactInteger(value uint64) error {
	if value == 0 || value > MaxExactInteger {
		return ErrInvalidRecordValue
	}
	return nil
}

func parseCanonicalDecimal(value string) (uint64, error) {
	decimal, err := ParseUnsignedDecimal(value)
	if err != nil {
		return 0, err
	}
	return decimal.Uint64()
}

func parsePositiveDecimal(value string) (uint64, error) {
	parsed, err := parseCanonicalDecimal(value)
	if err != nil || parsed == 0 {
		return 0, ErrInvalidRecordValue
	}
	return parsed, nil
}

func finalPagePublicationID(record FinalPageRecord) Digest {
	if !record.initialized || len(record.record) != FinalPageFieldCount {
		return ""
	}
	return Digest(record.record[9].Value)
}

func (record FinalImageRecord) Key() (string, error) {
	if err := record.validate(); err != nil {
		return "", err
	}
	return ImageDataKey(record.publicationID, record.normalizedPageURL, record.normalizedSourceURL)
}

func (record ImageManifestRecord) ImageKeys() []string {
	return append([]string(nil), record.imageKeys...)
}

func (record LegacyRetirementRecord) Input() (LegacyRetirementRecordInput, error) {
	if err := record.validate(); err != nil {
		return LegacyRetirementRecordInput{}, err
	}
	return record.input, nil
}

func (record AdminFreezeRecord) Input() (AdminFreezeRecordInput, error) {
	if err := record.validate(); err != nil {
		return AdminFreezeRecordInput{}, err
	}
	return record.input, nil
}

func (record DurabilityRecord) Input() (DurabilityRecordInput, error) {
	if err := record.validate(); err != nil {
		return DurabilityRecordInput{}, err
	}
	return record.input, nil
}

func decimalField(name string, value uint64) Field {
	return textField(name, strconv.FormatUint(value, 10))
}

func wrapRecordValueError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", ErrInvalidRecordValue, err)
}
