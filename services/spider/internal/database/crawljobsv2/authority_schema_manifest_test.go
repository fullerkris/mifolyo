package crawljobsv2

import (
	"reflect"
	"strings"
	"testing"
)

func TestAuthoritySchemaManifestPinsEveryRecordField(t *testing.T) {
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
		SchemaCompatibilityArtifact: {
			"manifest_version", "crawl_jobs", "crawl_policy", "canonicalization", "page_publication",
			"image_manifest", "backlink_projection", "render_ipc", "signal_queue",
			"global_request_concurrency", "redis_config_sha256", "commit_guard_sha256", "spider_image",
			"seed_importer_image", "crawl_admin_image", "indexer_image", "image_indexer_image",
			"backlinks_processor_image", "monitoring_image", "render_worker_image",
		},
		SchemaCompatibilityMarker: {
			"manifest_version", "manifest_sha256", "crawl_jobs", "crawl_policy", "canonicalization",
			"page_publication", "image_manifest", "backlink_projection", "render_ipc", "signal_queue",
			"global_request_concurrency", "redis_config_sha256", "commit_guard_sha256", "spider_image",
			"seed_importer_image", "crawl_admin_image", "indexer_image", "image_indexer_image",
			"backlinks_processor_image", "monitoring_image", "render_worker_image",
		},
		SchemaGuardCore: {
			"protocol_version", "contract_sha256", "redis_version", "redis_config_sha256",
			"maximum_shape_sha256", "memory_fixture_sha256", "lua_benchmark_sha256",
			"aof_crash_evidence_sha256", "cutover_mode", "candidate_run_id", "approved",
		},
		SchemaCommitGuard: {
			"protocol_version", "contract_sha256", "compatibility_manifest_sha256", "redis_version",
			"redis_config_sha256", "maximum_shape_sha256", "memory_fixture_sha256",
			"lua_benchmark_sha256", "aof_crash_evidence_sha256", "cutover_mode", "candidate_run_id",
			"approved_at_ms", "approved",
		},
		SchemaLegacyRetirement: {
			"protocol_version", "freeze_nonce", "backup_sha256", "v1_count", "v1_url_field_count",
			"v1_depth_field_count", "v1_source_sha256", "v1_queue_evidence_sha256",
			"v1_urls_evidence_sha256", "v1_depths_evidence_sha256", "spider_queue_type",
			"spider_queue_count", "spider_queue_evidence_sha256", "signal_queue_type",
			"signal_queue_count", "signal_queue_evidence_sha256", "deleted_bitmap", "retired_at_ms",
		},
		SchemaAdminFreeze: {
			"protocol_version", "freeze_nonce", "process_stop_evidence_sha256", "candidate_manifest_sha256",
			"candidate_contract_sha256", "created_at_ms",
		},
		SchemaDurability: {
			"schema_version", "boot_state", "approved_redis_run_id", "boot_epoch", "approved_at_ms",
			"planned_shutdown_nonce", "planned_shutdown_evidence_sha256", "last_approval_mode",
			"consumed_planned_shutdown_nonce", "rehearsal_evidence_sha256", "rehearsal_at_ms",
			"acknowledged_loss_bound",
		},
		SchemaFirstRequestStart: {
			"protocol_version", "run_id", "job_id", "lease_fence", "started_at_ms",
		},
		SchemaFinalPage: {
			"normalized_url", "html", "original_html", "content_type", "status_code", "last_crawled",
			"rendered", "render_policy_rule", "render_policy_sha256", "publication_id",
		},
		SchemaFinalImage: {
			"contract_version", "publication_id", "normalized_page_url", "normalized_source_url", "alt",
		},
		SchemaImageManifest: {
			"contract_version", "publication_id", "normalized_url", "image_count", "image_keys",
		},
	}

	schemas := []RecordSchema{
		SchemaRun,
		SchemaJob,
		SchemaReservation,
		SchemaRateScope,
		SchemaStageMeta,
		SchemaCompatibilityArtifact,
		SchemaCompatibilityMarker,
		SchemaGuardCore,
		SchemaCommitGuard,
		SchemaLegacyRetirement,
		SchemaAdminFreeze,
		SchemaDurability,
		SchemaFirstRequestStart,
		SchemaFinalPage,
		SchemaFinalImage,
		SchemaImageManifest,
	}
	if len(expected) != len(schemas) {
		t.Fatalf("literal schema manifest has %d entries, want %d", len(expected), len(schemas))
	}
	for _, schema := range schemas {
		want, ok := expected[schema]
		if !ok {
			t.Fatalf("literal schema manifest is missing %q", schema)
		}
		got, err := RecordSchemaFields(schema)
		if err != nil {
			t.Fatalf("schema %q: %v", schema, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("schema %q fields = %#v, want %#v", schema, got, want)
		}
	}
}

// TestAuthoritySchemaZeroSHA256Manifest is intentionally field-name driven and
// independent of the production validator index lists. It covers every fixed
// record schema, including schemas with optional digest fields and the schema
// with no digest fields. ValidateRecord is the production decoder/validator
// boundary; for authority records it encodes and dispatches through the exact
// concrete decoder.
func TestAuthoritySchemaZeroSHA256Manifest(t *testing.T) {
	manifest := map[RecordSchema][]string{
		SchemaRun: {
			"contract_sha256", "source_sha256", "authorization_sha256", "authorization_scope_sha256",
			"canonicalization_sha256", "crawl_policy_sha256", "render_policy_sha256",
			"policy_group_map_sha256", "archive_sha256", "purge_evidence_sha256",
		},
		SchemaJob: {
			"group_scope_id", "initial_origin_scope_id", "policy_decision_sha256",
			"last_document_target_digest", "active_reservation_id", "active_stage_commit_id",
			"last_stage_commit_id", "output_digest", "publication_id", "commit_id", "last_transition_id",
		},
		SchemaReservation: {
			"reservation_id", "target_digest", "crawl_policy_sha256", "policy_decision_sha256",
			"global_scope_id", "group_scope_id", "origin_scope_id",
		},
		SchemaRateScope: {
			"scope_id", "concurrency_source_sha256", "interval_source_sha256",
		},
		SchemaStageMeta: {
			"token_digest", "commit_id", "publication_id", "output_digest",
			"page_fields_chunk_digest", "html_chunk_digest", "original_html_chunk_digest",
			"outlinks_chunk_0_digest", "outlinks_chunk_1_digest", "outlinks_chunk_2_digest",
			"outlinks_chunk_3_digest", "discoveries_chunk_0_digest", "discoveries_chunk_1_digest",
			"aliases_chunk_0_digest", "images_chunk_0_digest", "manifest_chunk_digest",
		},
		SchemaCompatibilityArtifact: {
			"redis_config_sha256", "commit_guard_sha256", "spider_image", "seed_importer_image",
			"crawl_admin_image", "indexer_image", "image_indexer_image", "backlinks_processor_image",
			"monitoring_image", "render_worker_image",
		},
		SchemaCompatibilityMarker: {
			"manifest_sha256", "redis_config_sha256", "commit_guard_sha256", "spider_image",
			"seed_importer_image", "crawl_admin_image", "indexer_image", "image_indexer_image",
			"backlinks_processor_image", "monitoring_image", "render_worker_image",
		},
		SchemaGuardCore: {
			"contract_sha256", "redis_config_sha256", "maximum_shape_sha256", "memory_fixture_sha256",
			"lua_benchmark_sha256", "aof_crash_evidence_sha256",
		},
		SchemaCommitGuard: {
			"contract_sha256", "compatibility_manifest_sha256", "redis_config_sha256",
			"maximum_shape_sha256", "memory_fixture_sha256", "lua_benchmark_sha256",
			"aof_crash_evidence_sha256",
		},
		SchemaLegacyRetirement: {
			"backup_sha256", "v1_source_sha256", "v1_queue_evidence_sha256", "v1_urls_evidence_sha256",
			"v1_depths_evidence_sha256", "spider_queue_evidence_sha256", "signal_queue_evidence_sha256",
		},
		SchemaAdminFreeze: {
			"process_stop_evidence_sha256", "candidate_manifest_sha256", "candidate_contract_sha256",
		},
		SchemaDurability: {
			"planned_shutdown_evidence_sha256", "rehearsal_evidence_sha256",
		},
		SchemaFirstRequestStart: {},
		SchemaFinalPage: {
			"render_policy_sha256", "publication_id",
		},
		SchemaFinalImage: {
			"publication_id",
		},
		SchemaImageManifest: {
			"publication_id",
		},
	}

	records := authoritySchemaZeroSHA256Records(t)
	if len(manifest) != 16 || len(records) != len(manifest) {
		t.Fatalf("zero schema manifest = %d, valid records = %d, want 16", len(manifest), len(records))
	}
	for schema, digestFields := range manifest {
		base, covered := records[schema]
		if !covered {
			t.Fatalf("zero schema manifest has no valid base record for %q", schema)
		}
		if err := ValidateRecord(schema, base); err != nil {
			t.Fatalf("valid %q base record: %v", schema, err)
		}

		fieldNames, err := RecordSchemaFields(schema)
		if err != nil {
			t.Fatalf("fields for %q: %v", schema, err)
		}
		inferred := make([]string, 0, len(digestFields))
		fieldIndex := make(map[string]int, len(fieldNames))
		for index, name := range fieldNames {
			fieldIndex[name] = index
			if authoritySchemaFieldUsesSHA256Sentinel(name) {
				inferred = append(inferred, name)
			}
		}
		if !reflect.DeepEqual(inferred, digestFields) {
			t.Fatalf("%q zero fields = %v, literal manifest = %v", schema, inferred, digestFields)
		}

		for _, field := range digestFields {
			field := field
			t.Run(string(schema)+"/"+field, func(t *testing.T) {
				index, exists := fieldIndex[field]
				if !exists {
					t.Fatalf("literal zero field %q is not in schema %q", field, schema)
				}
				candidate := cloneRecord(base)
				zero := ZeroSHA256
				if strings.HasSuffix(field, "_image") {
					zero = "sha256:" + ZeroSHA256
				}
				candidate[index].Value = []byte(zero)
				if err := ValidateRecord(schema, candidate); err == nil {
					t.Fatal("production record boundary accepted ZERO_SHA256")
				}
			})
		}
	}
	for schema := range records {
		if _, covered := manifest[schema]; !covered {
			t.Fatalf("valid zero-test record has no literal manifest entry for %q", schema)
		}
	}
}

func authoritySchemaFieldUsesSHA256Sentinel(field string) bool {
	if strings.Contains(field, "sha256") || strings.HasSuffix(field, "_digest") || strings.HasSuffix(field, "_image") {
		return true
	}
	if field == "scope_id" || strings.HasSuffix(field, "_scope_id") && field != "rate_scope_id" {
		return true
	}
	switch field {
	case "reservation_id", "active_reservation_id", "active_stage_commit_id", "last_stage_commit_id",
		"commit_id", "publication_id", "last_transition_id":
		return true
	default:
		return false
	}
}

func authoritySchemaZeroSHA256Records(t *testing.T) map[RecordSchema]Record {
	t.Helper()
	records := map[RecordSchema]Record{
		SchemaRun:         recordAuthorityRunRecord(t, "archived"),
		SchemaJob:         recordAuthorityPublishedJobRecord(t),
		SchemaReservation: recordAuthorityReservationRecord(t, "started"),
		SchemaRateScope:   recordAuthorityRateScopeRecord(t),
		SchemaStageMeta:   recordAuthoritySealedStageRecord(t, "0"),
	}
	for _, testCase := range recordAuthorityCodecCases(t) {
		if _, duplicate := records[testCase.schema]; duplicate {
			t.Fatalf("duplicate valid zero-test record for %q", testCase.schema)
		}
		records[testCase.schema] = cloneRecord(testCase.record)
	}

	fixture := loadDigestVectorFixture(t)
	context := vectorOutputContextValue(t, fixture)
	output := vectorOutputValue(t, fixture)
	publicationID := mustFixtureDigest(t, fixture.Expected.PublicationID)
	finalPage, err := NewFinalPageRecord(context, output.Page, publicationID)
	if err != nil {
		t.Fatalf("construct final-page zero-test record: %v", err)
	}
	records[SchemaFinalPage], err = finalPage.Record()
	if err != nil {
		t.Fatalf("read final-page zero-test record: %v", err)
	}
	if len(output.Images) == 0 {
		t.Fatal("digest vector has no image for final-image zero test")
	}
	finalImage, err := NewFinalImageRecord(publicationID, context.finalTarget.CanonicalURL, output.Images[0])
	if err != nil {
		t.Fatalf("construct final-image zero-test record: %v", err)
	}
	records[SchemaFinalImage], err = finalImage.Record()
	if err != nil {
		t.Fatalf("read final-image zero-test record: %v", err)
	}
	imageManifest, err := NewImageManifestRecord(publicationID, context.finalTarget.CanonicalURL, output.Images)
	if err != nil {
		t.Fatalf("construct image-manifest zero-test record: %v", err)
	}
	records[SchemaImageManifest], err = imageManifest.Record()
	if err != nil {
		t.Fatalf("read image-manifest zero-test record: %v", err)
	}
	return records
}
