package crawljobsv2

import "errors"

type GateMode string

type SourceKind string

const (
	GateBootOnly  GateMode = "boot_only"
	GateCandidate GateMode = "candidate"
	GateActive    GateMode = "active"

	SourceMongo       SourceKind = "mongo"
	SourceV1Migration SourceKind = "v1_migration"
)

var (
	ErrInvalidGateMode   = errors.New("crawljobsv2: invalid gate mode")
	ErrInvalidSchema     = errors.New("crawljobsv2: invalid fixed-shape schema")
	ErrInvalidSourceKind = errors.New("crawljobsv2: invalid source kind")
)

type RecordSchema string

const (
	SchemaRun         RecordSchema = "run"
	SchemaJob         RecordSchema = "job"
	SchemaReservation RecordSchema = "reservation"
	SchemaRateScope   RecordSchema = "rate_scope"
	SchemaStageMeta   RecordSchema = "stage_meta"

	SchemaCompatibilityArtifact RecordSchema = "compatibility_artifact"
	SchemaCompatibilityMarker   RecordSchema = "compatibility_marker"
	SchemaGuardCore             RecordSchema = "guard_core"
	SchemaCommitGuard           RecordSchema = "commit_guard"
	SchemaLegacyRetirement      RecordSchema = "legacy_retirement"
	SchemaAdminFreeze           RecordSchema = "admin_freeze"
	SchemaDurability            RecordSchema = "durability"
	SchemaFirstRequestStart     RecordSchema = "first_request_start"
	SchemaFinalPage             RecordSchema = "final_page"
	SchemaFinalImage            RecordSchema = "final_image"
	SchemaImageManifest         RecordSchema = "image_manifest"
)

func TransportGateFields() []string {
	return []string{
		"gate_mode",
		"expected_boot_epoch",
		"expected_contract_sha256_or_empty",
		"expected_compatibility_record_or_empty",
		"expected_commit_guard_record_or_empty",
		"expected_legacy_retirement_record_or_empty",
		"expected_admin_freeze_record_or_empty",
	}
}

func OperationTransportGateFields(operation OperationName) ([]string, error) {
	if _, err := ParseOperationName(string(operation)); err != nil {
		return nil, err
	}
	if operation == OperationApproveBoot {
		return nil, nil
	}
	return TransportGateFields(), nil
}

func RecordSchemaFields(schema RecordSchema) ([]string, error) {
	switch schema {
	case SchemaRun:
		return []string{
			"protocol_version", "contract_sha256", "state", "source_kind", "source_sha256",
			"expected_seed_count", "authorization_sha256", "authorization_scope_sha256",
			"authorization_expires_at_ms", "canonicalization_version", "canonicalization_sha256",
			"crawl_policy_version", "crawl_policy_sha256", "render_policy_version",
			"render_policy_sha256", "policy_group_count", "policy_group_map_sha256", "max_jobs",
			"max_request_starts", "global_concurrency_limit", "max_delivery_attempts", "job_count",
			"open_job_count", "request_starts", "reservation_creations_total",
			"pending_request_reservations", "started_request_reservations", "claims_total",
			"retries_total", "recovered_leases_total", "renewal_rejections_total", "completed_total",
			"dead_total", "cancelled_total", "output_commits_total", "load_revision", "audit_revision",
			"audit_count", "audit_cursor", "audit_complete", "created_at_ms", "sealed_at_ms",
			"activated_at_ms", "budget_exhausted_at_ms", "cancelled_at_ms", "completed_at_ms",
			"finalized_at_ms", "last_activity_at_ms", "last_execution_at_ms",
			"last_request_started_at_ms", "last_terminal_transition_at_ms", "retention_anchor_ms",
			"archived_at_ms", "archive_sha256", "purge_state", "purge_evidence_sha256",
			"purge_started_at_ms", "purged_job_count", "terminal_reason",
		}, nil
	case SchemaJob:
		return []string{
			"protocol_version", "run_id", "job_id", "url_id", "canonical_url", "depth", "score_text",
			"state", "group_id", "rate_scope_id", "group_scope_id", "initial_origin_scope_id",
			"policy_decision_sha256", "claim_count", "delivery_attempts", "request_starts",
			"retry_count", "pre_io_recoveries", "next_request_ordinal", "last_request_started_at_ms",
			"last_document_request_started_at_ms", "last_document_request_fence",
			"last_document_target_url_id", "last_document_target_url", "last_document_target_digest",
			"last_reason", "last_failure_reason", "lease_owner", "lease_token", "lease_fence",
			"lease_started_at_ms", "lease_expires_at_ms", "lease_delivery_started",
			"active_reservation_id", "active_stage_commit_id", "last_stage_commit_id", "last_stage_fence",
			"not_before_ms", "commit_backpressure_fence", "commit_backpressure_reason",
			"commit_backpressure_started_at_ms", "commit_backpressure_deadline_ms", "output_digest",
			"publication_id", "commit_id", "published_page_key", "last_transition_id",
			"last_transition_status", "created_at_ms", "updated_at_ms", "completed_at_ms", "dead_at_ms",
			"cancelled_at_ms",
		}, nil
	case SchemaReservation:
		return []string{
			"protocol_version", "reservation_id", "run_id", "job_id", "owner_id", "lease_token",
			"lease_fence", "request_ordinal", "state", "request_kind", "target_url_id",
			"canonical_target_url", "target_digest", "crawl_policy_sha256", "policy_decision_sha256",
			"group_id", "rate_scope_id", "global_scope_id", "group_scope_id", "origin_scope_id",
			"global_concurrency", "global_interval_ms", "group_concurrency", "group_interval_ms",
			"origin_concurrency", "origin_interval_ms", "created_at_ms", "started_at_ms", "terminal_at_ms",
			"delivery_attempts_after_start", "job_starts_after_start", "run_starts_after_start",
			"group_starts_after_start", "expires_at_ms",
		}, nil
	case SchemaRateScope:
		return []string{
			"protocol_version", "scope_id", "scope_kind", "scope_witness", "effective_concurrency",
			"effective_interval_ms", "next_allowed_ms", "last_started_at_ms", "active_count",
			"pending_count", "started_count", "concurrency_source_sha256", "interval_source_sha256",
			"updated_at_ms",
		}, nil
	case SchemaStageMeta:
		return []string{
			"protocol_version", "run_id", "job_id", "owner_id", "lease_fence", "token_digest", "commit_id",
			"publication_id", "output_digest", "created_at_ms", "expires_at_ms", "sealed", "sealed_at_ms",
			"abandoned", "expected_page_fields", "expected_outlinks", "expected_discoveries",
			"expected_aliases", "expected_images", "page_fields_written", "html_written",
			"original_html_written", "outlinks_written", "discoveries_written", "aliases_written",
			"images_written", "manifest_written", "data_bytes", "key_count", "page_fields_chunk_digest",
			"html_chunk_digest", "original_html_chunk_digest", "outlinks_chunk_0_digest",
			"outlinks_chunk_1_digest", "outlinks_chunk_2_digest", "outlinks_chunk_3_digest",
			"discoveries_chunk_0_digest", "discoveries_chunk_1_digest", "aliases_chunk_0_digest",
			"images_chunk_0_digest", "manifest_chunk_digest",
		}, nil
	case SchemaCompatibilityArtifact:
		return compatibilityArtifactFieldNames(), nil
	case SchemaCompatibilityMarker:
		return compatibilityMarkerFieldNames(), nil
	case SchemaGuardCore:
		return guardCoreFieldNames(), nil
	case SchemaCommitGuard:
		return commitGuardFieldNames(), nil
	case SchemaLegacyRetirement:
		return legacyRetirementFieldNames(), nil
	case SchemaAdminFreeze:
		return adminFreezeFieldNames(), nil
	case SchemaDurability:
		return durabilityFieldNames(), nil
	case SchemaFirstRequestStart:
		return firstRequestStartFieldNames(), nil
	case SchemaFinalPage:
		return finalPageFieldNames(), nil
	case SchemaFinalImage:
		return finalImageFieldNames(), nil
	case SchemaImageManifest:
		return imageManifestFieldNames(), nil
	default:
		return nil, ErrInvalidSchema
	}
}

func ParseSourceKind(value string) (SourceKind, error) {
	kind := SourceKind(value)
	switch kind {
	case SourceMongo, SourceV1Migration:
		return kind, nil
	default:
		return "", ErrInvalidSourceKind
	}
}

func ValidateRecordSchema(schema RecordSchema, fields []string) error {
	expected, err := RecordSchemaFields(schema)
	if err != nil {
		return err
	}
	if len(fields) != len(expected) {
		return ErrInvalidSchema
	}
	for index := range expected {
		if fields[index] != expected[index] {
			return ErrInvalidSchema
		}
	}
	return nil
}

func AllowedGateModes(operation OperationName) ([]GateMode, error) {
	if _, err := ParseOperationName(string(operation)); err != nil {
		return nil, err
	}
	switch operation {
	case OperationApproveBoot:
		return nil, nil
	case OperationInstallCandidateMarkers:
		return []GateMode{GateBootOnly}, nil
	case OperationRetireLegacyKeys, OperationPromoteCandidateContracts:
		return []GateMode{GateCandidate}, nil
	case OperationCreateRun, OperationEnqueueBatch, OperationBeginRunAudit, OperationAuditRunBatch,
		OperationSealRun, OperationCancelRun, OperationCancelBatch, OperationPurgeRunBatch:
		return []GateMode{GateActive, GateCandidate}, nil
	default:
		return []GateMode{GateActive}, nil
	}
}

func ValidateGateMode(operation OperationName, mode GateMode) error {
	modes, err := AllowedGateModes(operation)
	if err != nil {
		return err
	}
	for _, allowed := range modes {
		if mode == allowed {
			return nil
		}
	}
	return ErrInvalidGateMode
}
