package crawljobsv2

import (
	"crypto/sha1" // #nosec G505 -- Redis SCRIPT LOAD/EVALSHA identity is normatively SHA-1.
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	ErrInvalidScriptBindingSet = errors.New("crawljobsv2: invalid script binding set")
	ErrScriptBindingMismatch   = errors.New("crawljobsv2: script binding mismatch")
	ErrScriptContractMismatch  = errors.New("crawljobsv2: script bundle contract mismatch")
	ErrInvalidOperationWire    = errors.New("crawljobsv2: invalid operation wire request")
	ErrOperationWireKeys       = errors.New("crawljobsv2: operation wire keys mismatch")
	ErrOperationWireArguments  = errors.New("crawljobsv2: operation wire arguments mismatch")
	ErrOperationWireRecords    = errors.New("crawljobsv2: operation wire records mismatch")
	ErrOperationWireChunk      = errors.New("crawljobsv2: operation wire chunk mismatch")
)

// ScriptBindingReview is retained as an opaque diagnostic compatibility type.
// It deliberately carries no source or digest fields and is not accepted by a
// production constructor. In particular, caller-provided review data can never
// become execution authority.
//
// Deprecated: authoritative scripts will be represented by ScriptBindingSet.
type ScriptBindingReview struct {
	sealed bool
}

// ScriptBindingRequirement describes the closed source identity expected for
// one operation. RequiredScriptBindings returns every operation exactly once in
// protocol order.
type ScriptBindingRequirement struct {
	Operation  OperationName
	SourceName string
}

type scriptBinding struct {
	operation    OperationName
	sourceName   string
	source       string
	redisSHA1    string
	sourceSHA256 Digest
}

// scriptBindingSetSeal pins the two bundle-level trust anchors. It is private so
// only source compiled into this package can seal a ScriptBindingSet.
type scriptBindingSetSeal struct {
	sourceSetSHA256        Digest
	approvedContractSHA256 Digest
	bundleSHA256           Digest
}

// ScriptBindingSet is the opaque, immutable complete authoritative Lua bundle.
// Its zero value is invalid and all representation fields are private. There is
// intentionally no production constructor yet: the future generated source
// file must embed the reviewed Lua bytes, list exactly one binding per operation
// in operationWireOrder, pin both per-source hashes, pin the ordered source-set
// digest and approved contract digest, and provide the matching private seal.
// Source bytes must be compile-time embedded, and the generator must emit the
// pinned identities and seal as package-private literals; production
// initialization must not derive authority from runtime input. This file
// intentionally declares neither a bundle value nor an approved contract digest.
// Until the generated file exists, production code has no way to obtain a valid
// bundle and BuildEvalSHARequest fails closed.
type ScriptBindingSet struct {
	bindings               []scriptBinding
	sourceSetSHA256        Digest
	approvedContractSHA256 Digest
	seal                   *scriptBindingSetSeal
}

func RequiredScriptBindings() []ScriptBindingRequirement {
	requirements := make([]ScriptBindingRequirement, len(operationWireOrder))
	for index, operation := range operationWireOrder {
		requirements[index] = ScriptBindingRequirement{
			Operation: operation, SourceName: canonicalScriptSourceName(operation),
		}
	}
	return requirements
}

func (set ScriptBindingSet) bindingFor(operation OperationName) (scriptBinding, error) {
	if err := set.validate(); err != nil {
		return scriptBinding{}, err
	}
	return set.bindingForValidated(operation)
}

func (set ScriptBindingSet) bindingForValidated(operation OperationName) (scriptBinding, error) {
	for index, expected := range operationWireOrder {
		if expected == operation {
			return set.bindings[index], nil
		}
	}
	return scriptBinding{}, ErrScriptBindingMismatch
}

func (set ScriptBindingSet) validate() error {
	if set.seal == nil || len(set.bindings) != len(operationWireOrder) ||
		!validScriptBundleDigest(set.sourceSetSHA256) || !validScriptBundleDigest(set.approvedContractSHA256) ||
		!validScriptBundleDigest(set.seal.sourceSetSHA256) || !validScriptBundleDigest(set.seal.approvedContractSHA256) ||
		!validScriptBundleDigest(set.seal.bundleSHA256) {
		return ErrInvalidScriptBindingSet
	}
	seenNames := make(map[string]struct{}, len(set.bindings))
	seenSources := make(map[string]struct{}, len(set.bindings))
	seenRedisSHA1s := make(map[string]struct{}, len(set.bindings))
	seenSourceSHA256s := make(map[Digest]struct{}, len(set.bindings))
	for index, operation := range operationWireOrder {
		binding := set.bindings[index]
		if binding.operation != operation || binding.sourceName != canonicalScriptSourceName(operation) ||
			len(binding.source) == 0 || !utf8.ValidString(binding.source) || !isLowerHex(binding.redisSHA1, 40) || !validScriptBundleDigest(binding.sourceSHA256) {
			return ErrInvalidScriptBindingSet
		}
		if _, duplicate := seenNames[binding.sourceName]; duplicate {
			return ErrInvalidScriptBindingSet
		}
		if _, duplicate := seenSources[binding.source]; duplicate {
			return ErrInvalidScriptBindingSet
		}
		if _, duplicate := seenRedisSHA1s[binding.redisSHA1]; duplicate {
			return ErrInvalidScriptBindingSet
		}
		if _, duplicate := seenSourceSHA256s[binding.sourceSHA256]; duplicate {
			return ErrInvalidScriptBindingSet
		}
		seenNames[binding.sourceName] = struct{}{}
		seenSources[binding.source] = struct{}{}
		seenRedisSHA1s[binding.redisSHA1] = struct{}{}
		seenSourceSHA256s[binding.sourceSHA256] = struct{}{}
		source := []byte(binding.source)
		redisDigest := sha1.Sum(source) // #nosec G401 -- required by Redis EVALSHA.
		sourceDigest := sha256.Sum256(source)
		if binding.redisSHA1 != hex.EncodeToString(redisDigest[:]) || binding.sourceSHA256 != Digest(hex.EncodeToString(sourceDigest[:])) {
			return ErrScriptBindingMismatch
		}
	}
	if set.sourceSetSHA256 != set.seal.sourceSetSHA256 ||
		set.approvedContractSHA256 != set.seal.approvedContractSHA256 ||
		set.sourceSetSHA256 != deriveScriptSourceSetDigest(set.bindings) ||
		set.seal.bundleSHA256 != deriveScriptBindingSetSeal(set.sourceSetSHA256, set.approvedContractSHA256) {
		return ErrScriptBindingMismatch
	}
	return nil
}

func (set ScriptBindingSet) validateRequestContract(request OperationWireRequest) error {
	if request.operation == OperationApproveBoot {
		return nil
	}
	if request.operation == OperationInstallCandidateMarkers {
		if Digest(operationWireSemanticValues(request.semantic)["contract_sha256"]) != set.approvedContractSHA256 {
			return ErrScriptContractMismatch
		}
		return nil
	}
	if !request.hasGate || Digest(request.gate.arguments[2]) != set.approvedContractSHA256 {
		return ErrScriptContractMismatch
	}
	return nil
}

// deriveScriptSourceSetDigest commits the count and, in protocol order, each
// index, operation, canonical name, exact source, Redis SHA-1, and source
// SHA-256 using the package's length-prefixed digest framing.
func deriveScriptSourceSetDigest(bindings []scriptBinding) Digest {
	values := make([][]byte, 0, 1+len(bindings)*6)
	values = append(values, []byte(strconv.Itoa(len(bindings))))
	for index, binding := range bindings {
		values = append(values,
			[]byte(strconv.Itoa(index)),
			[]byte(binding.operation),
			[]byte(binding.sourceName),
			[]byte(binding.source),
			[]byte(binding.redisSHA1),
			[]byte(binding.sourceSHA256),
		)
	}
	return digestFramed("mifolyo:crawl-jobs-v2:lua-source-set:v1", values...)
}

func deriveScriptBindingSetSeal(sourceSetSHA256, approvedContractSHA256 Digest) Digest {
	return digestFramed(
		"mifolyo:crawl-jobs-v2:lua-bundle-seal:v1",
		[]byte(sourceSetSHA256),
		[]byte(approvedContractSHA256),
	)
}

func validScriptBundleDigest(value Digest) bool {
	return value != Digest(ZeroSHA256) && validateDigest(value) == nil
}

func canonicalScriptSourceName(operation OperationName) string {
	return strings.ToLower(string(operation)) + ".lua"
}

type wireKeyPlan uint8

const (
	wireKeysApproveBoot wireKeyPlan = iota + 1
	wireKeysAuthority
	wireKeysRetire
	wireKeysPromote
	wireKeysMarkShutdown
	wireKeysCreateRun
	wireKeysRunRecords
	wireKeysRun
	wireKeysJob
	wireKeysReservation
	wireKeysStage
	wireKeysRunMaintenance
	wireKeysArchive
	wireKeysPurge
	wireKeysCleanStage
	wireKeysRateMaintenance
)

type wireTailKind uint8

const (
	wireTailNone wireTailKind = iota
	wireTailRecords
	wireTailActiveRunIDs
)

type operationWireSpecification struct {
	operation        OperationName
	gateModes        []GateMode
	semanticFields   []string
	tailKind         wireTailKind
	tailCountField   string
	recordFields     []string
	minimumRecords   uint64
	maximumRecords   uint64
	chunkKinds       []ChunkKind
	keyPlan          wireKeyPlan
	requestByteLimit uint64
}

var operationWireOrder = []OperationName{
	OperationApproveBoot,
	OperationInstallCandidateMarkers,
	OperationRetireLegacyKeys,
	OperationPromoteCandidateContracts,
	OperationMarkPlannedShutdown,
	OperationCreateRun,
	OperationEnqueueBatch,
	OperationBeginRunAudit,
	OperationAuditRunBatch,
	OperationSealRun,
	OperationActivateRun,
	OperationRejectReady,
	OperationTryClaim,
	OperationRenewLease,
	OperationReserveRequest,
	OperationStartRequest,
	OperationFinishRequest,
	OperationCancelReservation,
	OperationReleaseBeforeIO,
	OperationRetry,
	OperationDead,
	OperationCancelJob,
	OperationCompleteNoOutput,
	OperationBeginStage,
	OperationStagePageFields,
	OperationStagePageBlob,
	OperationStageOutlinksBatch,
	OperationStageDiscoveriesBatch,
	OperationStageAliasesBatch,
	OperationStageImagesBatch,
	OperationStageImageManifest,
	OperationAbortStage,
	OperationSealStage,
	OperationCommit,
	OperationPromoteDue,
	OperationRecoverExpired,
	OperationCancelRun,
	OperationCancelBatch,
	OperationFinalizeRun,
	OperationArchiveRun,
	OperationPurgeRunBatch,
	OperationCleanStage,
	OperationMaintainRateScopes,
}

var operationWireSpecifications = buildOperationWireSpecifications()

func buildOperationWireSpecifications() map[OperationName]operationWireSpecification {
	active := []GateMode{GateActive}
	candidate := []GateMode{GateCandidate}
	activeOrCandidate := []GateMode{GateActive, GateCandidate}
	sourceRecord := []string{
		"job_id", "canonical_url", "score_text", "depth", "group_id", "rate_scope_id",
		"group_scope_id", "initial_origin_scope_id", "policy_decision_sha256",
	}
	lease := []string{"run_id", "job_id", "owner_id", "lease_token", "fence"}
	stagePrefix := append(append([]string(nil), lease...), "commit_id", "chunk_kind", "chunk_ordinal", "chunk_digest", "record_count")
	specifications := []operationWireSpecification{
		wireSpec(OperationApproveBoot, nil, wireKeysApproveBoot, []string{
			"current_redis_run_id", "proposed_boot_epoch", "evidence_sha256", "evidence_at_ms", "loss_bound",
			"planned_nonce_or_empty", "planned_shutdown_evidence_sha256_or_empty", "approval_mode",
		}),
		wireSpec(OperationInstallCandidateMarkers, []GateMode{GateBootOnly}, wireKeysAuthority,
			append([]string{"freeze_nonce", "process_stop_evidence_sha256", "contract_sha256"}, compatibilityMarkerFieldNames()...)),
		wireSpec(OperationRetireLegacyKeys, candidate, wireKeysRetire, []string{
			"freeze_nonce", "backup_sha256", "v1_count", "v1_url_field_count", "v1_depth_field_count",
			"v1_source_sha256", "v1_queue_evidence_sha256", "v1_urls_evidence_sha256", "v1_depths_evidence_sha256",
			"spider_queue_type", "spider_queue_count", "spider_queue_evidence_sha256", "signal_queue_type",
			"signal_queue_count", "signal_queue_evidence_sha256", "confirmation_text",
		}),
		wireSpec(OperationPromoteCandidateContracts, candidate, wireKeysPromote, []string{
			"freeze_nonce", "commit_guard_sha256", "protocol_version", "contract_sha256", "redis_version",
			"redis_config_sha256", "maximum_shape_sha256", "memory_fixture_sha256", "lua_benchmark_sha256",
			"aof_crash_evidence_sha256", "cutover_mode", "candidate_run_id", "approved",
		}),
		wireTailSpec(OperationMarkPlannedShutdown, active, wireKeysMarkShutdown,
			[]string{"planned_shutdown_nonce", "process_stop_evidence_sha256", "active_run_count"},
			wireTailActiveRunIDs, "active_run_count", nil, 0, MaxActiveRuns),
		wireRecordSpec(OperationCreateRun, activeOrCandidate, wireKeysCreateRun, []string{
			"run_id", "source_kind", "source_sha256", "expected_seed_count", "authorization_sha256",
			"authorization_scope_sha256", "authorization_expires_at_ms", "canonicalization_version",
			"canonicalization_sha256", "crawl_policy_version", "crawl_policy_sha256", "render_policy_version",
			"render_policy_sha256", "policy_group_map_sha256", "max_jobs", "max_request_starts",
			"global_concurrency_limit", "max_delivery_attempts", "policy_group_count",
		}, "policy_group_count", []string{
			"group_id", "rate_scope_id", "group_scope_id", "request_start_limit", "concurrency", "interval_ms",
		}, 1, MaxPolicyGroupsPerRun),
		wireRecordSpec(OperationEnqueueBatch, activeOrCandidate, wireKeysRunRecords,
			[]string{"run_id", "record_count"}, "record_count", sourceRecord, 1, FeederEnqueueBatchSize),
		wireSpec(OperationBeginRunAudit, activeOrCandidate, wireKeysRun, []string{"run_id"}),
		wireRecordSpec(OperationAuditRunBatch, activeOrCandidate, wireKeysRunRecords,
			[]string{"run_id", "expected_prior_cursor", "expected_prior_count", "record_count"},
			"record_count", sourceRecord, 0, RunAuditBatchSize),
		wireSpec(OperationSealRun, activeOrCandidate, wireKeysRun,
			[]string{"run_id", "expected_job_count", "source_sha256"}),
		wireSpec(OperationActivateRun, active, wireKeysRun,
			[]string{"run_id", "confirmation_text", "authorization_sha256", "crawl_policy_sha256", "render_policy_sha256", "canonicalization_sha256"}),
		wireSpec(OperationRejectReady, active, wireKeysJob, []string{
			"run_id", "job_id", "canonical_url", "score_text", "depth", "group_id", "rate_scope_id",
			"group_scope_id", "initial_origin_scope_id", "policy_decision_sha256", "reason", "transition_id",
		}),
		wireSpec(OperationTryClaim, active, wireKeysReservation, []string{
			"run_id", "job_id", "canonical_url", "score_text", "depth", "job_group_id", "job_rate_scope_id",
			"job_group_scope_id", "job_initial_origin_scope_id", "job_policy_decision_sha256", "expected_prior_fence",
			"fence", "owner_id", "lease_token", "request_ordinal", "request_kind", "target_url_id",
			"canonical_target_url", "target_digest", "crawl_policy_sha256", "policy_decision_sha256", "group_id",
			"rate_scope_id", "global_scope_id", "group_scope_id", "origin_scope_id", "global_concurrency",
			"global_interval_ms", "group_concurrency", "group_interval_ms", "origin_concurrency", "origin_interval_ms",
			"transition_id",
		}),
		wireSpec(OperationRenewLease, active, wireKeysJob, lease),
		wireSpec(OperationReserveRequest, active, wireKeysReservation, append(append([]string(nil), lease...),
			"request_ordinal", "request_kind", "target_url_id", "canonical_target_url", "target_digest",
			"crawl_policy_sha256", "policy_decision_sha256", "group_id", "rate_scope_id", "global_scope_id",
			"group_scope_id", "origin_scope_id", "global_concurrency", "global_interval_ms", "group_concurrency",
			"group_interval_ms", "origin_concurrency", "origin_interval_ms")),
		wireSpec(OperationStartRequest, active, wireKeysReservation, append(append([]string(nil), lease...), "reservation_id")),
		wireSpec(OperationFinishRequest, active, wireKeysReservation, append(append([]string(nil), lease...), "reservation_id")),
		wireSpec(OperationCancelReservation, active, wireKeysReservation, append(append([]string(nil), lease...), "reservation_id")),
		wireSpec(OperationReleaseBeforeIO, active, wireKeysJob, append(append([]string(nil), lease...), "transition_id")),
		wireSpec(OperationRetry, active, wireKeysJob, append(append([]string(nil), lease...), "reason", "transition_id")),
		wireSpec(OperationDead, active, wireKeysJob, append(append([]string(nil), lease...), "reason", "transition_id")),
		wireSpec(OperationCancelJob, active, wireKeysJob, append(append([]string(nil), lease...), "reason", "transition_id")),
		wireSpec(OperationCompleteNoOutput, active, wireKeysJob, append(append([]string(nil), lease...), "reason", "transition_id")),
		wireSpec(OperationBeginStage, active, wireKeysStage, append(append([]string(nil), lease...),
			"commit_id", "publication_id", "output_digest", "request_starts_baseline", "request_starts_generation", "expected_page_fields", "expected_outlinks",
			"expected_discoveries", "expected_aliases", "expected_images")),
		wireStageSpec(OperationStagePageFields, stagePrefix, []string{
			"normalized_url", "content_type", "status_code", "last_crawled", "rendered", "render_policy_rule",
			"render_policy_sha256", "publication_id",
		}, 1, 1, ChunkPageFields),
		wireStageSpec(OperationStagePageBlob, stagePrefix, []string{"field_name", "field_bytes"}, 1, 1, ChunkHTML, ChunkOriginalHTML),
		wireStageSpec(OperationStageOutlinksBatch, stagePrefix, []string{"target_url"}, 1, MaxNonBlobStageBatchRecords, ChunkOutlinks),
		wireStageSpec(OperationStageDiscoveriesBatch, stagePrefix, []string{
			"job_id", "canonical_url", "depth", "score_text", "group_id", "rate_scope_id", "group_scope_id",
			"initial_origin_scope_id", "policy_decision_sha256",
		}, 1, MaxNonBlobStageBatchRecords, ChunkDiscoveries),
		wireStageSpec(OperationStageAliasesBatch, stagePrefix, []string{"url_id", "canonical_url", "depth"}, 1, MaxAliasesPerJob, ChunkAliases),
		wireStageSpec(OperationStageImagesBatch, stagePrefix, []string{"normalized_source_url", "alt"}, 1, MaxImagesPerPage, ChunkImages),
		wireStageSpec(OperationStageImageManifest, stagePrefix,
			[]string{"contract_version", "publication_id", "normalized_url", "image_count", "image_keys"}, 1, 1, ChunkImageManifest),
		wireSpec(OperationAbortStage, active, wireKeysStage, append(append([]string(nil), lease...), "commit_id", "transition_id")),
		wireSpec(OperationSealStage, active, wireKeysStage, append(append([]string(nil), lease...),
			"commit_id", "verified_output_digest", "verified_manifest_chunk_digest")),
		wireSpec(OperationCommit, active, wireKeysStage, append(append([]string(nil), lease...), "commit_id")),
		wireSpec(OperationPromoteDue, active, wireKeysRunMaintenance, []string{"run_id"}),
		wireSpec(OperationRecoverExpired, active, wireKeysRunMaintenance, []string{"run_id"}),
		wireSpec(OperationCancelRun, activeOrCandidate, wireKeysRun, []string{"run_id", "reason"}),
		wireSpec(OperationCancelBatch, activeOrCandidate, wireKeysRunMaintenance, []string{"run_id"}),
		wireSpec(OperationFinalizeRun, active, wireKeysRunMaintenance, []string{"run_id"}),
		wireSpec(OperationArchiveRun, active, wireKeysArchive, []string{"run_id", "archive_sha256", "confirmation_text"}),
		wireSpec(OperationPurgeRunBatch, activeOrCandidate, wireKeysPurge,
			[]string{"run_id", "evidence_sha256", "expected_first_job_id_or_empty"}),
		wireSpec(OperationCleanStage, active, wireKeysCleanStage,
			[]string{"expected_commit_id", "expected_cleanup_due_at_ms"}),
		wireSpec(OperationMaintainRateScopes, active, wireKeysRateMaintenance, []string{"rank_offset"}),
	}

	result := make(map[OperationName]operationWireSpecification, len(specifications))
	for _, specification := range specifications {
		if _, duplicate := result[specification.operation]; duplicate {
			panic("duplicate Crawl Jobs V2 operation wire specification")
		}
		result[specification.operation] = specification
	}
	if len(result) != len(operationWireOrder) {
		panic("incomplete Crawl Jobs V2 operation wire specification")
	}
	return result
}

func wireSpec(operation OperationName, modes []GateMode, keys wireKeyPlan, fields []string) operationWireSpecification {
	return operationWireSpecification{
		operation: operation, gateModes: append([]GateMode(nil), modes...), semanticFields: append([]string(nil), fields...),
		keyPlan: keys, requestByteLimit: operationWireRequestLimit(operation),
	}
}

func wireTailSpec(
	operation OperationName,
	modes []GateMode,
	keys wireKeyPlan,
	fields []string,
	tail wireTailKind,
	countField string,
	recordFields []string,
	minimum uint64,
	maximum uint64,
) operationWireSpecification {
	specification := wireSpec(operation, modes, keys, fields)
	specification.tailKind = tail
	specification.tailCountField = countField
	specification.recordFields = append([]string(nil), recordFields...)
	specification.minimumRecords = minimum
	specification.maximumRecords = maximum
	return specification
}

func wireRecordSpec(
	operation OperationName,
	modes []GateMode,
	keys wireKeyPlan,
	fields []string,
	countField string,
	recordFields []string,
	minimum uint64,
	maximum uint64,
) operationWireSpecification {
	return wireTailSpec(operation, modes, keys, fields, wireTailRecords, countField, recordFields, minimum, maximum)
}

func wireStageSpec(operation OperationName, fields, recordFields []string, minimum, maximum uint64, kinds ...ChunkKind) operationWireSpecification {
	specification := wireRecordSpec(operation, []GateMode{GateActive}, wireKeysStage, fields, "record_count", recordFields, minimum, maximum)
	specification.chunkKinds = append([]ChunkKind(nil), kinds...)
	return specification
}

func operationWireRequestLimit(operation OperationName) uint64 {
	switch operation {
	case OperationStagePageBlob:
		return MaxPageBlobEvalSHARequestBytes
	case OperationCommit:
		return MaxCommitEvalSHARequestBytes
	case OperationStagePageFields, OperationStageOutlinksBatch, OperationStageDiscoveriesBatch,
		OperationStageAliasesBatch, OperationStageImagesBatch, OperationStageImageManifest:
		return MaxNonBlobStageBatchRequestBytes
	default:
		return MaxOrdinaryEvalSHARequestBytes
	}
}
