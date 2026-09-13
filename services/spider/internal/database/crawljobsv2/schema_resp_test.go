package crawljobsv2

import (
	"errors"
	"strconv"
	"testing"
)

func TestFixedRecordSchemasMatchNormativeFieldOrder(t *testing.T) {
	expected := map[RecordSchema][]string{
		SchemaRun: {
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
		},
		SchemaJob: {
			"protocol_version", "run_id", "job_id", "url_id", "canonical_url", "depth", "score_text",
			"state", "group_id", "rate_scope_id", "group_scope_id", "initial_origin_scope_id",
			"policy_decision_sha256", "claim_count", "delivery_attempts", "request_starts",
			"lease_request_starts_baseline",
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
		},
		SchemaReservation: {
			"protocol_version", "reservation_id", "run_id", "job_id", "owner_id", "lease_token",
			"lease_fence", "request_ordinal", "state", "request_kind", "target_url_id",
			"canonical_target_url", "target_digest", "crawl_policy_sha256", "policy_decision_sha256",
			"group_id", "rate_scope_id", "global_scope_id", "group_scope_id", "origin_scope_id",
			"global_concurrency", "global_interval_ms", "group_concurrency", "group_interval_ms",
			"origin_concurrency", "origin_interval_ms", "created_at_ms", "started_at_ms", "terminal_at_ms",
			"delivery_attempts_after_start", "job_starts_after_start", "run_starts_after_start",
			"group_starts_after_start", "expires_at_ms",
		},
		SchemaRateScope: {
			"protocol_version", "scope_id", "scope_kind", "scope_witness", "effective_concurrency",
			"effective_interval_ms", "next_allowed_ms", "last_started_at_ms", "active_count",
			"pending_count", "started_count", "concurrency_source_sha256", "interval_source_sha256",
			"updated_at_ms",
		},
		SchemaStageMeta: {
			"protocol_version", "run_id", "job_id", "owner_id", "lease_fence", "token_digest", "commit_id",
			"publication_id", "output_digest", "request_starts_baseline", "request_starts_generation",
			"created_at_ms", "expires_at_ms", "sealed", "sealed_at_ms",
			"abandoned", "expected_page_fields", "expected_outlinks", "expected_discoveries",
			"expected_aliases", "expected_images", "page_fields_written", "html_written",
			"original_html_written", "outlinks_written", "discoveries_written", "aliases_written",
			"images_written", "manifest_written", "data_bytes", "key_count", "page_fields_chunk_digest",
			"html_chunk_digest", "original_html_chunk_digest", "outlinks_chunk_0_digest",
			"outlinks_chunk_1_digest", "outlinks_chunk_2_digest", "outlinks_chunk_3_digest",
			"discoveries_chunk_0_digest", "discoveries_chunk_1_digest", "aliases_chunk_0_digest",
			"images_chunk_0_digest", "manifest_chunk_digest",
		},
	}
	for schema, want := range expected {
		got, err := RecordSchemaFields(schema)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want) {
			t.Fatalf("schema %q field count = %d, want %d", schema, len(got), len(want))
		}
		for index := range want {
			if got[index] != want[index] {
				t.Fatalf("schema %q field %d = %q, want %q", schema, index, got[index], want[index])
			}
		}
		if err := ValidateRecordSchema(schema, got); err != nil {
			t.Fatalf("schema %q rejected itself: %v", schema, err)
		}
		got[0] = "mutated"
		fresh, err := RecordSchemaFields(schema)
		if err != nil || fresh[0] != "protocol_version" {
			t.Fatalf("schema %q returned shared mutable storage", schema)
		}
	}
	if _, err := RecordSchemaFields(RecordSchema("unknown")); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("unknown schema error = %v", err)
	}
}

func TestClosedTransportGateAndOperationModeMapping(t *testing.T) {
	wantPrefix := []string{
		"gate_mode", "expected_boot_epoch", "expected_contract_sha256_or_empty",
		"expected_compatibility_record_or_empty", "expected_commit_guard_record_or_empty",
		"expected_legacy_retirement_record_or_empty", "expected_admin_freeze_record_or_empty",
	}
	got := TransportGateFields()
	for index := range wantPrefix {
		if got[index] != wantPrefix[index] {
			t.Fatalf("transport gate field %d = %q", index, got[index])
		}
	}
	withoutPrefix, err := OperationTransportGateFields(OperationApproveBoot)
	if err != nil || withoutPrefix != nil {
		t.Fatalf("approve boot prefix = %#v, err=%v", withoutPrefix, err)
	}
	withPrefix, err := OperationTransportGateFields(OperationRetry)
	if err != nil || len(withPrefix) != len(wantPrefix) {
		t.Fatalf("runtime prefix length = %d, err=%v", len(withPrefix), err)
	}

	candidateOperations := map[OperationName]struct{}{
		OperationCreateRun: {}, OperationEnqueueBatch: {}, OperationBeginRunAudit: {},
		OperationAuditRunBatch: {}, OperationSealRun: {}, OperationCancelRun: {},
		OperationCancelBatch: {}, OperationPurgeRunBatch: {},
	}
	for operation := range operations {
		modes, err := AllowedGateModes(operation)
		if err != nil {
			t.Fatal(err)
		}
		switch operation {
		case OperationApproveBoot:
			if modes != nil {
				t.Fatalf("approve boot modes = %#v", modes)
			}
		case OperationInstallCandidateMarkers:
			assertModes(t, operation, modes, []GateMode{GateBootOnly})
		case OperationRetireLegacyKeys, OperationPromoteCandidateContracts:
			assertModes(t, operation, modes, []GateMode{GateCandidate})
		default:
			if _, candidate := candidateOperations[operation]; candidate {
				assertModes(t, operation, modes, []GateMode{GateActive, GateCandidate})
			} else {
				assertModes(t, operation, modes, []GateMode{GateActive})
			}
		}
	}
	if err := ValidateGateMode(OperationRetry, GateCandidate); !errors.Is(err, ErrInvalidGateMode) {
		t.Fatalf("runtime candidate mode error = %v", err)
	}
	if err := ValidateGateMode(OperationCreateRun, GateCandidate); err != nil {
		t.Fatalf("candidate create mode: %v", err)
	}
}

func assertModes(t *testing.T, operation OperationName, got, want []GateMode) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("operation %q mode count = %d, want %d", operation, len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("operation %q mode %d = %q, want %q", operation, index, got[index], want[index])
		}
	}
}

func TestExactRESP2EvalSHASerializedSize(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	keys := [][]byte{[]byte("key"), {0x00, 0xff}}
	arguments := [][]byte{[]byte(""), []byte("argument"), {0x00, '\r', '\n', 0xff}}
	expectedCommand := respCommandForTest(sha, keys, arguments)
	got, err := EvalSHASerializedSize(sha, keys, arguments)
	if err != nil {
		t.Fatal(err)
	}
	if got != uint64(len(expectedCommand)) {
		t.Fatalf("serialized size = %d, want %d", got, len(expectedCommand))
	}
	if _, err := EvalSHASerializedSize(stringsOfLength(40, 'A'), nil, nil); !errors.Is(err, ErrInvalidScriptSHA1) {
		t.Fatalf("uppercase SHA-1 error = %v", err)
	}
}

func TestEvalSHAOperationLimitsAndRecordBounds(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	limitCases := []struct {
		operation   OperationName
		recordCount uint64
		limit       uint64
	}{
		{OperationRetry, 0, MaxOrdinaryEvalSHARequestBytes},
		{OperationStagePageBlob, 1, MaxPageBlobEvalSHARequestBytes},
		{OperationStageOutlinksBatch, 1, MaxNonBlobStageBatchRequestBytes},
		{OperationCommit, 0, MaxCommitEvalSHARequestBytes},
	}
	for _, test := range limitCases {
		t.Run(string(test.operation), func(t *testing.T) {
			argumentLength := exactSingleArgumentLength(t, sha, test.limit)
			argument := make([]byte, argumentLength)
			size, err := validateEvalSHARequest(test.operation, sha, nil, [][]byte{argument}, test.recordCount)
			if err != nil || size != test.limit {
				t.Fatalf("exact limit: size=%d err=%v want=%d", size, err, test.limit)
			}
			argument = append(argument, 0)
			size, err = validateEvalSHARequest(test.operation, sha, nil, [][]byte{argument}, test.recordCount)
			if !errors.Is(err, ErrCommandBoundsExceeded) || size <= test.limit {
				t.Fatalf("over limit: size=%d err=%v", size, err)
			}
		})
	}

	recordCases := []struct {
		operation OperationName
		valid     []uint64
		invalid   []uint64
	}{
		{OperationEnqueueBatch, []uint64{1, FeederEnqueueBatchSize}, []uint64{0, FeederEnqueueBatchSize + 1}},
		{OperationAuditRunBatch, []uint64{0, RunAuditBatchSize}, []uint64{RunAuditBatchSize + 1}},
		{OperationStagePageFields, []uint64{1}, []uint64{0, 2}},
		{OperationStagePageBlob, []uint64{1}, []uint64{0, 2}},
		{OperationStageOutlinksBatch, []uint64{1, 64}, []uint64{0, 65}},
		{OperationStageDiscoveriesBatch, []uint64{1, 64}, []uint64{0, 65}},
		{OperationStageAliasesBatch, []uint64{1, 5}, []uint64{0, 6}},
		{OperationStageImagesBatch, []uint64{1, 64}, []uint64{0, 65}},
		{OperationStageImageManifest, []uint64{1}, []uint64{0, 2}},
	}
	for _, test := range recordCases {
		for _, count := range test.valid {
			if _, err := validateEvalSHARequest(test.operation, sha, nil, nil, count); err != nil {
				t.Fatalf("operation %q valid count %d: %v", test.operation, count, err)
			}
		}
		for _, count := range test.invalid {
			if _, err := validateEvalSHARequest(test.operation, sha, nil, nil, count); !errors.Is(err, ErrRecordBoundsExceeded) {
				t.Fatalf("operation %q invalid count %d: %v", test.operation, count, err)
			}
		}
	}
}

func respCommandForTest(sha string, keys, arguments [][]byte) []byte {
	parts := make([][]byte, 0, 3+len(keys)+len(arguments))
	parts = append(parts, []byte("EVALSHA"), []byte(sha), []byte(strconv.Itoa(len(keys))))
	parts = append(parts, keys...)
	parts = append(parts, arguments...)
	command := []byte("*" + strconv.Itoa(len(parts)) + "\r\n")
	for _, part := range parts {
		command = append(command, []byte("$"+strconv.Itoa(len(part))+"\r\n")...)
		command = append(command, part...)
		command = append(command, '\r', '\n')
	}
	return command
}

func exactSingleArgumentLength(t *testing.T, sha string, limit uint64) int {
	t.Helper()
	for candidate := int(limit); candidate >= int(limit)-128; candidate-- {
		size, err := EvalSHASerializedSize(sha, nil, [][]byte{make([]byte, candidate)})
		if err != nil {
			t.Fatal(err)
		}
		if size == limit {
			return candidate
		}
	}
	t.Fatalf("could not construct exact %d-byte EVALSHA request", limit)
	return 0
}

func stringsOfLength(length int, value byte) string {
	result := make([]byte, length)
	for index := range result {
		result[index] = value
	}
	return string(result)
}
