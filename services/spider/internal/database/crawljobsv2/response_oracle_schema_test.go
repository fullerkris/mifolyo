package crawljobsv2

import (
	"errors"
	"slices"
	"testing"
)

// literalResponseSchemaOracle is intentionally independent of
// responseTailFields, claimResponseFields, and the other production schema
// helpers. Each allowed operation/status pair and its complete ordered envelope
// is written literally so accidental widening, narrowing, or reordering is
// observable here.
var literalResponseSchemaOracle = map[OperationName]map[Status][]string{
	OperationApproveBoot: {
		StatusOK:              {"status", "now_ms", "boot_epoch"},
		StatusExistsIdentical: {"status", "now_ms", "boot_epoch"},
	},
	OperationMarkPlannedShutdown: {
		StatusOK:              {"status", "now_ms", "planned_nonce"},
		StatusExistsIdentical: {"status", "now_ms", "planned_nonce"},
	},
	OperationInstallCandidateMarkers: {
		StatusCandidateInstalled: {"status", "now_ms", "manifest_sha256", "contract_sha256"},
		StatusExistsIdentical:    {"status", "now_ms", "manifest_sha256", "contract_sha256"},
	},
	OperationRetireLegacyKeys: {
		StatusLegacyRetired:   {"status", "now_ms", "deleted_bitmap", "source_sha256"},
		StatusExistsIdentical: {"status", "now_ms", "deleted_bitmap", "source_sha256"},
	},
	OperationPromoteCandidateContracts: {
		StatusContractsPromoted: {"status", "now_ms", "manifest_sha256", "contract_sha256", "commit_guard_sha256"},
		StatusExistsIdentical:   {"status", "now_ms", "manifest_sha256", "contract_sha256", "commit_guard_sha256"},
	},
	OperationCreateRun: {
		StatusCreated:         {"status", "now_ms", "run_id"},
		StatusExistsIdentical: {"status", "now_ms", "run_id"},
	},
	OperationEnqueueBatch: {
		StatusOK:              {"status", "now_ms", "new_jobs", "reconciled_jobs", "job_count", "load_revision"},
		StatusExistsIdentical: {"status", "now_ms", "new_jobs", "reconciled_jobs", "job_count", "load_revision"},
	},
	OperationBeginRunAudit: {
		StatusAuditStarted:    {"status", "now_ms", "audit_revision", "job_count"},
		StatusExistsIdentical: {"status", "now_ms", "audit_revision", "job_count"},
	},
	OperationAuditRunBatch: {
		StatusBatchMore: {"status", "now_ms", "checked", "audit_count", "audit_cursor_or_empty"},
		StatusBatchDone: {"status", "now_ms", "checked", "audit_count", "audit_cursor_or_empty"},
	},
	OperationSealRun: {
		StatusSealed:          {"status", "now_ms", "job_count", "source_sha256"},
		StatusExistsIdentical: {"status", "now_ms", "job_count", "source_sha256"},
	},
	OperationActivateRun: {
		StatusActivated:       {"status", "now_ms", "activated_at_ms"},
		StatusExistsIdentical: {"status", "now_ms", "activated_at_ms"},
	},
	OperationRejectReady: {
		StatusDead:                 {"status", "now_ms", "reason"},
		StatusAuthorizationExpired: {"status", "now_ms"},
		StatusRunCancelled:         {"status", "now_ms"},
	},
	OperationTryClaim: {
		StatusClaimed:                      {"status", "now_ms", "fence", "lease_expires_at_ms", "reservation_id", "reservation_expires_at_ms"},
		StatusAlreadyClaimed:               {"status", "now_ms", "fence", "lease_expires_at_ms", "reservation_id", "reservation_expires_at_ms"},
		StatusVisitedCompleted:             {"status", "now_ms", "completed_at_ms"},
		StatusCapacityBlocked:              {"status", "now_ms", "scope_id", "active_count", "effective_concurrency", "after_io"},
		StatusRateBlocked:                  {"status", "now_ms", "scope_id", "next_allowed_ms", "after_io"},
		StatusRunBudgetExhausted:           {"status", "now_ms", "started", "pending", "limit", "after_io"},
		StatusRunReservationLimitExhausted: {"status", "now_ms", "reservation_creations_total", "maximum_reservation_creations", "after_io"},
		StatusGroupBudgetExhausted:         {"status", "now_ms", "group_id", "started", "pending", "limit", "after_io"},
		StatusLeaseCapacityBlocked:         {"status", "now_ms", "active_leases", "maximum_active_leases"},
		StatusStageCapacityBlocked:         {"status", "now_ms", "blocked_reason", "active_stage_slots", "maximum_stage_slots"},
		StatusNoCandidate:                  {"status", "now_ms"},
		StatusAuthorizationExpired:         {"status", "now_ms"},
		StatusRunCancelled:                 {"status", "now_ms"},
		StatusLeaseLost:                    {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationRenewLease: {
		StatusRenewed:              {"status", "now_ms", "lease_expires_at_ms"},
		StatusAuthorizationExpired: {"status", "now_ms"},
		StatusRunCancelled:         {"status", "now_ms"},
		StatusLeaseLost:            {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationReserveRequest: {
		StatusReserved:                     {"status", "now_ms", "reservation_id", "expires_at_ms"},
		StatusAlreadyReserved:              {"status", "now_ms", "reservation_id", "expires_at_ms"},
		StatusCapacityBlocked:              {"status", "now_ms", "scope_id", "active_count", "effective_concurrency", "after_io"},
		StatusRateBlocked:                  {"status", "now_ms", "scope_id", "next_allowed_ms", "after_io"},
		StatusRunBudgetExhausted:           {"status", "now_ms", "started", "pending", "limit", "after_io"},
		StatusRunReservationLimitExhausted: {"status", "now_ms", "reservation_creations_total", "maximum_reservation_creations", "after_io"},
		StatusGroupBudgetExhausted:         {"status", "now_ms", "group_id", "started", "pending", "limit", "after_io"},
		StatusAuthorizationExpired:         {"status", "now_ms"},
		StatusRunCancelled:                 {"status", "now_ms"},
		StatusLeaseLost:                    {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationStartRequest: {
		StatusStarted:              {"status", "now_ms", "reservation_id", "started_at_ms", "delivery_attempts", "job_request_starts", "run_request_starts", "group_request_starts", "io_permission"},
		StatusAlreadyStarted:       {"status", "now_ms", "reservation_id", "started_at_ms", "delivery_attempts", "job_request_starts", "run_request_starts", "group_request_starts", "io_permission"},
		StatusRateBlocked:          {"status", "now_ms", "scope_id", "next_allowed_ms", "after_io"},
		StatusAuthorizationExpired: {"status", "now_ms"},
		StatusRunCancelled:         {"status", "now_ms"},
		StatusLeaseLost:            {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationFinishRequest: {
		StatusFinished:        {"status", "now_ms", "reservation_id"},
		StatusAlreadyFinished: {"status", "now_ms", "reservation_id"},
		StatusLeaseLost:       {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationCancelReservation: {
		StatusReservationCancelled: {"status", "now_ms", "reservation_id"},
		StatusLeaseLost:            {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationReleaseBeforeIO: {
		StatusReleasedReady:        {"status", "now_ms", "ready_at_ms"},
		StatusAuthorizationExpired: {"status", "now_ms"},
		StatusRunCancelled:         {"status", "now_ms"},
		StatusLeaseLost:            {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationRetry: {
		StatusRetryScheduled: {"status", "now_ms", "not_before_ms", "delivery_attempts", "reason"},
		StatusDead:           {"status", "now_ms", "dead_at_ms", "retry_exhausted", "last_failure_reason"},
		StatusCancelled:      {"status", "now_ms", "cancelled_at_ms", "reason"},
		StatusLeaseLost:      {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationDead: {
		StatusDead:      {"status", "now_ms", "terminal_at_ms", "reason"},
		StatusCancelled: {"status", "now_ms", "terminal_at_ms", "reason"},
		StatusLeaseLost: {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationCancelJob: {
		StatusCancelled: {"status", "now_ms", "terminal_at_ms", "reason"},
		StatusLeaseLost: {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationCompleteNoOutput: {
		StatusCompleted: {"status", "now_ms", "terminal_at_ms", "reason"},
		StatusCancelled: {"status", "now_ms", "terminal_at_ms", "reason"},
		StatusLeaseLost: {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationBeginStage: {
		StatusStageBegun:           {"status", "now_ms", "commit_id", "expires_at_ms", "memory_reservation_remaining_bytes"},
		StatusExistsIdentical:      {"status", "now_ms", "commit_id", "expires_at_ms", "memory_reservation_remaining_bytes"},
		StatusStageCapacityBlocked: {"status", "now_ms", "blocked_reason", "active_stage_slots", "maximum_stage_slots"},
		StatusAuthorizationExpired: {"status", "now_ms"},
		StatusRunCancelled:         {"status", "now_ms"},
		StatusLeaseLost:            {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationStagePageFields: {
		StatusStaged:               {"status", "now_ms", "commit_id", "chunk_kind", "chunk_ordinal", "accepted_records", "data_bytes", "key_count", "memory_reservation_remaining_bytes"},
		StatusExistsIdentical:      {"status", "now_ms", "commit_id", "chunk_kind", "chunk_ordinal", "accepted_records", "data_bytes", "key_count", "memory_reservation_remaining_bytes"},
		StatusAuthorizationExpired: {"status", "now_ms"},
		StatusRunCancelled:         {"status", "now_ms"},
		StatusLeaseLost:            {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationStagePageBlob: {
		StatusStaged:               {"status", "now_ms", "commit_id", "chunk_kind", "chunk_ordinal", "accepted_records", "data_bytes", "key_count", "memory_reservation_remaining_bytes"},
		StatusExistsIdentical:      {"status", "now_ms", "commit_id", "chunk_kind", "chunk_ordinal", "accepted_records", "data_bytes", "key_count", "memory_reservation_remaining_bytes"},
		StatusAuthorizationExpired: {"status", "now_ms"},
		StatusRunCancelled:         {"status", "now_ms"},
		StatusLeaseLost:            {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationStageOutlinksBatch: {
		StatusStaged:               {"status", "now_ms", "commit_id", "chunk_kind", "chunk_ordinal", "accepted_records", "data_bytes", "key_count", "memory_reservation_remaining_bytes"},
		StatusExistsIdentical:      {"status", "now_ms", "commit_id", "chunk_kind", "chunk_ordinal", "accepted_records", "data_bytes", "key_count", "memory_reservation_remaining_bytes"},
		StatusAuthorizationExpired: {"status", "now_ms"},
		StatusRunCancelled:         {"status", "now_ms"},
		StatusLeaseLost:            {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationStageDiscoveriesBatch: {
		StatusStaged:               {"status", "now_ms", "commit_id", "chunk_kind", "chunk_ordinal", "accepted_records", "data_bytes", "key_count", "memory_reservation_remaining_bytes"},
		StatusExistsIdentical:      {"status", "now_ms", "commit_id", "chunk_kind", "chunk_ordinal", "accepted_records", "data_bytes", "key_count", "memory_reservation_remaining_bytes"},
		StatusAuthorizationExpired: {"status", "now_ms"},
		StatusRunCancelled:         {"status", "now_ms"},
		StatusLeaseLost:            {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationStageAliasesBatch: {
		StatusStaged:               {"status", "now_ms", "commit_id", "chunk_kind", "chunk_ordinal", "accepted_records", "data_bytes", "key_count", "memory_reservation_remaining_bytes"},
		StatusExistsIdentical:      {"status", "now_ms", "commit_id", "chunk_kind", "chunk_ordinal", "accepted_records", "data_bytes", "key_count", "memory_reservation_remaining_bytes"},
		StatusAuthorizationExpired: {"status", "now_ms"},
		StatusRunCancelled:         {"status", "now_ms"},
		StatusLeaseLost:            {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationStageImagesBatch: {
		StatusStaged:               {"status", "now_ms", "commit_id", "chunk_kind", "chunk_ordinal", "accepted_records", "data_bytes", "key_count", "memory_reservation_remaining_bytes"},
		StatusExistsIdentical:      {"status", "now_ms", "commit_id", "chunk_kind", "chunk_ordinal", "accepted_records", "data_bytes", "key_count", "memory_reservation_remaining_bytes"},
		StatusAuthorizationExpired: {"status", "now_ms"},
		StatusRunCancelled:         {"status", "now_ms"},
		StatusLeaseLost:            {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationStageImageManifest: {
		StatusStaged:               {"status", "now_ms", "commit_id", "chunk_kind", "chunk_ordinal", "accepted_records", "data_bytes", "key_count", "memory_reservation_remaining_bytes"},
		StatusExistsIdentical:      {"status", "now_ms", "commit_id", "chunk_kind", "chunk_ordinal", "accepted_records", "data_bytes", "key_count", "memory_reservation_remaining_bytes"},
		StatusAuthorizationExpired: {"status", "now_ms"},
		StatusRunCancelled:         {"status", "now_ms"},
		StatusLeaseLost:            {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationSealStage: {
		StatusSealed:               {"status", "now_ms", "commit_id", "data_bytes", "key_count"},
		StatusExistsIdentical:      {"status", "now_ms", "commit_id", "data_bytes", "key_count"},
		StatusAuthorizationExpired: {"status", "now_ms"},
		StatusRunCancelled:         {"status", "now_ms"},
		StatusLeaseLost:            {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationAbortStage: {
		StatusStageAborted:    {"status", "now_ms", "commit_id", "unlinked_keys"},
		StatusExistsIdentical: {"status", "now_ms", "commit_id", "unlinked_keys"},
		StatusLeaseLost:       {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationCommit: {
		StatusCommitted:              {"status", "now_ms", "publication_id", "commit_id", "completed_at_ms"},
		StatusAlreadyCommitted:       {"status", "now_ms", "publication_id", "commit_id", "completed_at_ms"},
		StatusDownstreamBackpressure: {"status", "now_ms", "blocked_reason", "blocked_started_at_ms", "blocked_deadline_ms"},
		StatusAuthorizationExpired:   {"status", "now_ms"},
		StatusRunCancelled:           {"status", "now_ms"},
		StatusLeaseLost:              {"status", "now_ms", "current_fence_or_zero"},
	},
	OperationPromoteDue: {
		StatusBatchMore: {"status", "now_ms", "processed", "more"},
		StatusBatchDone: {"status", "now_ms", "processed", "more"},
	},
	OperationRecoverExpired: {
		StatusBatchMore: {"status", "now_ms", "processed", "more"},
		StatusBatchDone: {"status", "now_ms", "processed", "more"},
	},
	OperationCancelRun: {
		StatusCancelled:       {"status", "now_ms", "cancelled_at_ms", "reason"},
		StatusExistsIdentical: {"status", "now_ms", "cancelled_at_ms", "reason"},
	},
	OperationCancelBatch: {
		StatusBatchMore: {"status", "now_ms", "processed", "more"},
		StatusBatchDone: {"status", "now_ms", "processed", "more"},
	},
	OperationFinalizeRun: {
		StatusCompleted:                    {"status", "now_ms", "finalized_at_ms_or_zero", "terminal_reason"},
		StatusRunBudgetExhausted:           {"status", "now_ms", "finalized_at_ms_or_zero", "terminal_reason"},
		StatusRunReservationLimitExhausted: {"status", "now_ms", "finalized_at_ms_or_zero", "terminal_reason"},
		StatusGroupBudgetExhausted:         {"status", "now_ms", "finalized_at_ms_or_zero", "terminal_reason"},
		StatusCancelled:                    {"status", "now_ms", "finalized_at_ms_or_zero", "terminal_reason"},
		StatusNotDue:                       {"status", "now_ms", "finalized_at_ms_or_zero", "terminal_reason"},
	},
	OperationArchiveRun: {
		StatusArchived:        {"status", "now_ms", "archived_at_ms", "archive_sha256"},
		StatusExistsIdentical: {"status", "now_ms", "archived_at_ms", "archive_sha256"},
		StatusNotDue:          {"status", "now_ms", "eligible_at_ms"},
	},
	OperationPurgeRunBatch: {
		StatusBatchMore: {"status", "now_ms", "removed_jobs", "more"},
		StatusPurged:    {"status", "now_ms", "removed_jobs", "more"},
		StatusNotDue:    {"status", "now_ms", "eligible_at_ms"},
	},
	OperationCleanStage: {
		StatusBatchMore: {"status", "now_ms", "processed", "more"},
		StatusBatchDone: {"status", "now_ms", "processed", "more"},
	},
	OperationMaintainRateScopes: {
		StatusBatchMore: {"status", "now_ms", "processed", "more"},
		StatusBatchDone: {"status", "now_ms", "processed", "more"},
	},
}

func TestResponseSchemasMatchIndependentLiteralOracle(t *testing.T) {
	for operation := range operations {
		expectedByStatus, operationCovered := literalResponseSchemaOracle[operation]
		if !operationCovered {
			t.Fatalf("literal response oracle omits operation %s", operation)
		}
		for status := range statuses {
			expectedFields, allowed := expectedByStatus[status]
			schema, err := ResponseSchemaFor(operation, status)
			if !allowed {
				if !errors.Is(err, ErrResponseStatus) {
					t.Fatalf("%s/%s was not rejected: schema=%v err=%v", operation, status, schema.Fields, err)
				}
				continue
			}
			if err != nil {
				t.Fatalf("literal allowed pair %s/%s rejected: %v", operation, status, err)
			}
			if schema.Operation != operation || schema.Status != status || !slices.Equal(schema.Fields, expectedFields) {
				t.Fatalf("%s/%s schema = %v, want %v", operation, status, schema.Fields, expectedFields)
			}
		}
	}

	for operation, expectedByStatus := range literalResponseSchemaOracle {
		if _, known := operations[operation]; !known {
			t.Fatalf("literal response oracle contains unknown operation %s", operation)
		}
		for status := range expectedByStatus {
			if _, known := statuses[status]; !known {
				t.Fatalf("literal response oracle contains unknown status %s for %s", status, operation)
			}
		}
	}
}

// TestResponseZeroSHA256Manifest pins every digest-bearing response field in
// the independent literal schema oracle. The per-occurrence assertion proves
// that every allowed operation/status envelope routes the field through the
// nonzero scalar validator; the full-envelope cases below exercise each unique
// field name through the exported production boundary.
func TestResponseZeroSHA256Manifest(t *testing.T) {
	expectedOccurrences := map[string]int{
		"manifest_sha256":     4,
		"contract_sha256":     4,
		"source_sha256":       4,
		"commit_guard_sha256": 2,
		"reservation_id":      9,
		"scope_id":            5,
		"commit_id":           22,
		"publication_id":      2,
		"archive_sha256":      2,
	}
	actualOccurrences := make(map[string]int, len(expectedOccurrences))
	for operation, byStatus := range literalResponseSchemaOracle {
		for status, fields := range byStatus {
			for _, field := range fields {
				if !responseFieldUsesSHA256Sentinel(field) {
					continue
				}
				actualOccurrences[field]++
				if err := validateResponseField(field, ZeroSHA256, status); !errors.Is(err, ErrResponseScalar) {
					t.Fatalf("%s/%s field %q zero error = %v", operation, status, field, err)
				}
			}
		}
	}
	if !mapsEqualStringInt(actualOccurrences, expectedOccurrences) {
		t.Fatalf("response zero-field occurrences = %v, want %v", actualOccurrences, expectedOccurrences)
	}

	const now = "1788266097000"
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fullEnvelopeCases := []struct {
		name      string
		operation OperationName
		field     string
		index     int
		raw       []string
	}{
		{
			name: "manifest SHA-256", operation: OperationInstallCandidateMarkers,
			field: "manifest_sha256", index: 2,
			raw: []string{string(StatusCandidateInstalled), now, digest, digest},
		},
		{
			name: "contract SHA-256", operation: OperationInstallCandidateMarkers,
			field: "contract_sha256", index: 3,
			raw: []string{string(StatusCandidateInstalled), now, digest, digest},
		},
		{
			name: "source SHA-256", operation: OperationRetireLegacyKeys,
			field: "source_sha256", index: 3,
			raw: []string{string(StatusLegacyRetired), now, "10101", digest},
		},
		{
			name: "commit-guard SHA-256", operation: OperationPromoteCandidateContracts,
			field: "commit_guard_sha256", index: 4,
			raw: []string{string(StatusContractsPromoted), now, digest, digest, digest},
		},
		{
			name: "reservation ID", operation: OperationReserveRequest,
			field: "reservation_id", index: 2,
			raw: []string{string(StatusReserved), now, digest, "1788266157000"},
		},
		{
			name: "scope ID", operation: OperationReserveRequest,
			field: "scope_id", index: 2,
			raw: []string{string(StatusCapacityBlocked), now, digest, "2", "2", "0"},
		},
		{
			name: "commit ID", operation: OperationBeginStage,
			field: "commit_id", index: 2,
			raw: []string{string(StatusStageBegun), now, digest, "1788266997000", "50000000"},
		},
		{
			name: "publication ID", operation: OperationCommit,
			field: "publication_id", index: 2,
			raw: []string{string(StatusCommitted), now, digest, digest, now},
		},
		{
			name: "archive SHA-256", operation: OperationArchiveRun,
			field: "archive_sha256", index: 3,
			raw: []string{string(StatusArchived), now, now, digest},
		},
	}
	if len(fullEnvelopeCases) != len(expectedOccurrences) {
		t.Fatalf("full response zero cases = %d, unique fields = %d", len(fullEnvelopeCases), len(expectedOccurrences))
	}
	seenFields := make(map[string]struct{}, len(fullEnvelopeCases))
	for _, testCase := range fullEnvelopeCases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, duplicate := seenFields[testCase.field]; duplicate {
				t.Fatalf("duplicate full-envelope zero case for %q", testCase.field)
			}
			seenFields[testCase.field] = struct{}{}
			if err := ValidateOperationResponse(testCase.operation, testCase.raw); err != nil {
				t.Fatalf("valid response fixture: %v", err)
			}
			candidate := append([]string(nil), testCase.raw...)
			candidate[testCase.index] = ZeroSHA256
			if err := ValidateOperationResponse(testCase.operation, candidate); !errors.Is(err, ErrResponseScalar) {
				t.Fatalf("ZERO_SHA256 response error = %v", err)
			}
		})
	}
}

func responseFieldUsesSHA256Sentinel(field string) bool {
	if field == "reservation_id" || field == "scope_id" || field == "commit_id" || field == "publication_id" {
		return true
	}
	return len(field) > len("_sha256") && field[len(field)-len("_sha256"):] == "_sha256"
}

func mapsEqualStringInt(left, right map[string]int) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func TestBlockedBudgetResponsesRequireExactStartedPendingLimitRelation(t *testing.T) {
	const now = "1788266097000"
	tests := []struct {
		name      string
		operation OperationName
		valid     []string
		mutations map[string]struct {
			index int
			value string
		}
	}{
		{
			name:      "claim run budget",
			operation: OperationTryClaim,
			valid:     []string{string(StatusRunBudgetExhausted), now, "7", "3", "10", "0"},
			mutations: map[string]struct {
				index int
				value string
			}{
				"started below exact sum": {index: 2, value: "6"},
				"pending below exact sum": {index: 3, value: "2"},
				"limit differs from sum":  {index: 4, value: "9"},
				"zero limit":              {index: 4, value: "0"},
				"limit above maximum":     {index: 4, value: "11"},
			},
		},
		{
			name:      "reserve run budget",
			operation: OperationReserveRequest,
			valid:     []string{string(StatusRunBudgetExhausted), now, "7", "3", "10", "0"},
			mutations: map[string]struct {
				index int
				value string
			}{
				"started below exact sum": {index: 2, value: "6"},
				"pending below exact sum": {index: 3, value: "2"},
				"limit differs from sum":  {index: 4, value: "9"},
				"zero limit":              {index: 4, value: "0"},
				"limit above maximum":     {index: 4, value: "11"},
			},
		},
		{
			name:      "claim group budget",
			operation: OperationTryClaim,
			valid:     []string{string(StatusGroupBudgetExhausted), now, "group-a", "6", "4", "10", "0"},
			mutations: map[string]struct {
				index int
				value string
			}{
				"started below exact sum": {index: 3, value: "5"},
				"pending below exact sum": {index: 4, value: "3"},
				"limit differs from sum":  {index: 5, value: "9"},
				"zero limit":              {index: 5, value: "0"},
				"limit above maximum":     {index: 5, value: "11"},
			},
		},
		{
			name:      "reserve group budget",
			operation: OperationReserveRequest,
			valid:     []string{string(StatusGroupBudgetExhausted), now, "group-a", "6", "4", "10", "0"},
			mutations: map[string]struct {
				index int
				value string
			}{
				"started below exact sum": {index: 3, value: "5"},
				"pending below exact sum": {index: 4, value: "3"},
				"limit differs from sum":  {index: 5, value: "9"},
				"zero limit":              {index: 5, value: "0"},
				"limit above maximum":     {index: 5, value: "11"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateOperationResponse(test.operation, test.valid); err != nil {
				t.Fatalf("valid blocked-budget relation: %v", err)
			}
			for name, mutation := range test.mutations {
				t.Run(name, func(t *testing.T) {
					response := append([]string(nil), test.valid...)
					response[mutation.index] = mutation.value
					if err := ValidateOperationResponse(test.operation, response); !errors.Is(err, ErrResponseScalar) {
						t.Fatalf("mutated blocked-budget relation error = %v", err)
					}
				})
			}
		})
	}
}
