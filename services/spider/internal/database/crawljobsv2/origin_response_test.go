package crawljobsv2

import (
	"errors"
	"strings"
	"testing"
)

func TestCanonicalOriginDerivation(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want CanonicalOrigin
	}{
		{name: "HTTP default", url: "http://example.com/path", want: "http://example.com:80"},
		{name: "HTTPS default", url: "https://example.com/path?q=1", want: "https://example.com:443"},
		{name: "non-default", url: "https://example.com:8443/path", want: "https://example.com:8443"},
		{name: "ASCII IDNA", url: "https://xn--bcher-kva.de/path", want: "https://xn--bcher-kva.de:443"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := DeriveCanonicalOrigin(test.url)
			if err != nil || got != test.want {
				t.Fatalf("origin did not match: err=%v", err)
			}
			if err := validateCanonicalOrigin(got); err != nil {
				t.Fatalf("derived origin did not validate: %v", err)
			}
		})
	}
}

func TestCanonicalOriginRejectsUnsafeOrNoncanonicalURLs(t *testing.T) {
	tests := []string{
		"https://example.com:443/path",
		"HTTPS://example.com/path",
		"https://user:password@example.com/path",
		"ftp://example.com/path",
		"https://127.0.0.1/path",
		"https://[2001:db8::1]/path",
		"//example.com/path",
	}
	for _, input := range tests {
		if _, err := DeriveCanonicalOrigin(input); err == nil {
			t.Fatal("unsafe or noncanonical URL was accepted")
		} else if strings.Contains(err.Error(), input) || strings.Contains(err.Error(), "password") {
			t.Fatalf("origin error exposed URL data: %v", err)
		}
	}
	if _, err := DeriveOriginScopeID(CanonicalOrigin("https://example.com")); !errors.Is(err, ErrInvalidCanonicalOrigin) {
		t.Fatalf("origin without explicit port error = %v", err)
	}
}

func TestParseStartRequestResponseVariants(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	intent := vectorReservationValue(t, fixture, false)
	runPolicy := vectorRunPolicyAuthority(t, fixture, plainSHA256(testDenyAllRenderPolicyArtifact()))
	now := "1788266097000"
	startedRaw := []any{
		"STARTED", now, fixture.Expected.ReservationID, now,
		"1", "1", "7", "4", "1",
	}
	started, err := newTestTransportAuthority().parseStartRequestResponse(runPolicy, intent, startedRaw)
	if err != nil {
		t.Fatalf("parse STARTED: %v", err)
	}
	details, ok := started.StartedDetails()
	if started.Status() != StatusStarted || started.NowMS() != 1_788_266_097_000 || !ok {
		t.Fatal("STARTED union shape is invalid")
	}
	if string(details.ReservationID()) != fixture.Expected.ReservationID ||
		details.StartedAtMS() != 1_788_266_097_000 ||
		details.DeliveryAttempts() != 1 || details.JobRequestStarts() != 1 ||
		details.RunRequestStarts() != 7 || details.GroupRequestStarts() != 4 ||
		!details.IOPermission() {
		t.Fatal("STARTED details did not parse exactly")
	}
	for _, mutation := range []struct {
		name   string
		status Status
	}{
		{name: "new start", status: StatusStarted},
		{name: "replayed start", status: StatusAlreadyStarted},
	} {
		t.Run("job starts exceed reservation ordinal on "+mutation.name, func(t *testing.T) {
			raw := append([]any(nil), startedRaw...)
			raw[0] = string(mutation.status)
			raw[5] = "2"
			if _, err := newTestTransportAuthority().parseStartRequestResponse(runPolicy, intent, raw); !errors.Is(err, ErrResponseScalar) {
				t.Fatalf("ordinal authority mutation error = %v", err)
			}
		})
	}

	already := append([]any(nil), startedRaw...)
	already[0] = "ALREADY_STARTED"
	already[8] = "0"
	parsedAlready, err := newTestTransportAuthority().parseStartRequestResponse(runPolicy, intent, already)
	alreadyDetails, ok := parsedAlready.StartedDetails()
	if err != nil || !ok || alreadyDetails.IOPermission() {
		t.Fatalf("parse ALREADY_STARTED reconciliation: %v", err)
	}
	if _, err := parsedAlready.IOPermit(); !errors.Is(err, ErrInvalidResponseAuthority) {
		t.Fatalf("reconciliation response I/O permit error = %v", err)
	}

	rateRaw := []string{"RATE_BLOCKED", now, fixture.Expected.ScopeIDs["origin"], "1788266097250", "1"}
	rateBlocked, err := newTestTransportAuthority().parseStartRequestResponse(runPolicy, intent, rateRaw)
	blockedDetails, ok := rateBlocked.RateBlockedDetails()
	if err != nil || !ok || !blockedDetails.AfterIO() ||
		blockedDetails.NextAllowedMS() != 1_788_266_097_250 {
		t.Fatalf("parse RATE_BLOCKED: %v", err)
	}

	leaseLost, err := newTestTransportAuthority().parseStartRequestResponse(runPolicy, intent, []string{"LEASE_LOST", now, "0"})
	lostDetails, ok := leaseLost.LeaseLostDetails()
	if err != nil || !ok || lostDetails.CurrentFence() != 0 {
		t.Fatalf("parse LEASE_LOST: %v", err)
	}
	for _, status := range []Status{StatusAuthorizationExpired, StatusRunCancelled} {
		response, err := newTestTransportAuthority().parseStartRequestResponse(runPolicy, intent, []string{string(status), now})
		if _, ok := response.StartedDetails(); err != nil || ok {
			t.Fatalf("parse definitive status %q: %v", status, err)
		}
	}
}

func TestOperationResponseSchemasAreClosedAndExact(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	digest := fixture.Expected.CommitID
	reservationID := fixture.Expected.ReservationID
	now := "1788266097000"
	tests := []struct {
		operation OperationName
		raw       []string
	}{
		{OperationApproveBoot, []string{"OK", now, fixture.Identities.RunID}},
		{OperationMarkPlannedShutdown, []string{"OK", now, fixture.Identities.OwnerID}},
		{OperationInstallCandidateMarkers, []string{"CANDIDATE_INSTALLED", now, digest, digest}},
		{OperationRetireLegacyKeys, []string{"LEGACY_RETIRED", now, "10101", digest}},
		{OperationPromoteCandidateContracts, []string{"CONTRACTS_PROMOTED", now, digest, digest, digest}},
		{OperationCreateRun, []string{"CREATED", now, fixture.Identities.RunID}},
		{OperationEnqueueBatch, []string{"OK", now, "1", "0", "2", "3"}},
		{OperationBeginRunAudit, []string{"AUDIT_STARTED", now, "3", "2"}},
		{OperationAuditRunBatch, []string{"BATCH_MORE", now, "1", "1", fixture.Identities.JobID}},
		{OperationSealRun, []string{"SEALED", now, "2", digest}},
		{OperationActivateRun, []string{"ACTIVATED", now, now}},
		{OperationRejectReady, []string{"DEAD", now, "policy_denied"}},
		{OperationTryClaim, []string{"CLAIMED", now, "7", "1788266157000", reservationID, "1788266157000"}},
		{OperationRenewLease, []string{"RENEWED", now, "1788266157000"}},
		{OperationReserveRequest, []string{"RESERVED", now, reservationID, "1788266157000"}},
		{OperationStartRequest, []string{"STARTED", now, reservationID, now, "1", "1", "1", "1", "1"}},
		{OperationFinishRequest, []string{"FINISHED", now, reservationID}},
		{OperationCancelReservation, []string{"RESERVATION_CANCELLED", now, reservationID}},
		{OperationReleaseBeforeIO, []string{"RELEASED_READY", now, now}},
		{OperationRetry, []string{"RETRY_SCHEDULED", now, "1788266127000", "1", "request_timeout"}},
		{OperationDead, []string{"DEAD", now, now, "http_4xx"}},
		{OperationCancelJob, []string{"CANCELLED", now, now, "source_cancelled"}},
		{OperationCompleteNoOutput, []string{"COMPLETED", now, now, "already_visited"}},
		{OperationBeginStage, []string{"STAGE_BEGUN", now, digest, "1788266997000", "50000000"}},
		{OperationStagePageFields, []string{"STAGED", now, digest, "page_fields", "0", "1", "100", "3", "49900000"}},
		{OperationStagePageBlob, []string{"STAGED", now, digest, "html", "0", "1", "100", "3", "49900000"}},
		{OperationStageOutlinksBatch, []string{"STAGED", now, digest, "outlinks", "3", "64", "100", "3", "49900000"}},
		{OperationStageDiscoveriesBatch, []string{"STAGED", now, digest, "discoveries", "1", "64", "100", "3", "49900000"}},
		{OperationStageAliasesBatch, []string{"STAGED", now, digest, "aliases", "0", "5", "100", "3", "49900000"}},
		{OperationStageImagesBatch, []string{"STAGED", now, digest, "images", "0", "64", "100", "3", "49900000"}},
		{OperationStageImageManifest, []string{"STAGED", now, digest, "image_manifest", "0", "1", "100", "3", "49900000"}},
		{OperationAbortStage, []string{"STAGE_ABORTED", now, digest, "3"}},
		{OperationSealStage, []string{"SEALED", now, digest, "100", "5"}},
		{OperationCommit, []string{"COMMITTED", now, fixture.Expected.PublicationID, digest, now}},
		{OperationPromoteDue, []string{"BATCH_MORE", now, "1", "1"}},
		{OperationRecoverExpired, []string{"BATCH_DONE", now, "1", "0"}},
		{OperationCancelRun, []string{"CANCELLED", now, now, "operator_cancelled"}},
		{OperationCancelBatch, []string{"BATCH_DONE", now, "1", "0"}},
		{OperationFinalizeRun, []string{"COMPLETED", now, now, "all_jobs_terminal"}},
		{OperationArchiveRun, []string{"ARCHIVED", now, now, digest}},
		{OperationPurgeRunBatch, []string{"PURGED", now, "1", "0"}},
		{OperationCleanStage, []string{"BATCH_DONE", now, "1", "0"}},
		{OperationMaintainRateScopes, []string{"BATCH_DONE", now, "1", "0"}},
	}
	for _, test := range tests {
		t.Run(string(test.operation), func(t *testing.T) {
			var validationErr error
			if test.operation == OperationRenewLease {
				validationErr = ValidateRenewLeaseResponse(NewUnstagedRenewLeaseResponseContext(), test.raw)
			} else {
				validationErr = ValidateOperationResponse(test.operation, test.raw)
			}
			if validationErr != nil {
				t.Fatalf("valid response rejected: %v", validationErr)
			}
			status, err := ParseStatus(test.raw[0])
			if err != nil {
				t.Fatal(err)
			}
			schema, err := ResponseSchemaFor(test.operation, status)
			if err != nil || len(schema.Fields) != len(test.raw) {
				t.Fatalf("schema length mismatch: schema=%v err=%v", schema.Fields, err)
			}
			original := schema.Fields[0]
			schema.Fields[0] = "mutated"
			fresh, err := ResponseSchemaFor(test.operation, status)
			if err != nil || fresh.Fields[0] != original {
				t.Fatal("response schema returned shared mutable fields")
			}
		})
	}

	if err := ValidateOperationResponse(OperationTryClaim, []string{"STARTED", now, reservationID, now, "1", "1", "1", "1", "1"}); !errors.Is(err, ErrResponseStatus) {
		t.Fatalf("cross-operation status error = %v", err)
	}
	if err := ValidateOperationResponse(OperationStagePageFields, []string{"STAGED", now, digest, "images", "0", "1", "1", "1", "1"}); !errors.Is(err, ErrResponseScalar) {
		t.Fatalf("cross-stage kind error = %v", err)
	}
	if err := ValidateOperationResponse(OperationTryClaim, []string{"CAPACITY_BLOCKED", now, digest, "2", "2", "1"}); !errors.Is(err, ErrResponseScalar) {
		t.Fatalf("claim after_io relation error = %v", err)
	}
	if err := ValidateOperationResponse(OperationStartRequest, []string{"RATE_BLOCKED", now, digest, now, "0"}); !errors.Is(err, ErrResponseScalar) {
		t.Fatalf("start rate deadline relation error = %v", err)
	}
	// Claim/reserve can remain blocked by an unrecovered pending member even
	// after that member's logical deadline is due.
	if err := ValidateOperationResponse(OperationReserveRequest, []string{"RATE_BLOCKED", now, digest, "1788266096000", "0"}); err != nil {
		t.Fatalf("valid stale-pending rate block: %v", err)
	}
	invalidRelations := []struct {
		operation OperationName
		response  []string
	}{
		{OperationTryClaim, []string{"CLAIMED", now, "7", "1788266157000", reservationID, "1788266157001"}},
		{OperationRenewLease, []string{"RENEWED", now, now}},
		{OperationRetry, []string{"RETRY_SCHEDULED", now, "1788266217000", "3", "request_timeout"}},
		{OperationDead, []string{"DEAD", now, "1788266097001", "http_4xx"}},
		{OperationBeginStage, []string{"STAGE_BEGUN", now, digest, "1788266997000", "65535"}},
		{OperationCommit, []string{"DOWNSTREAM_BACKPRESSURE", now, "pages_queue_full", "1788266097001", "1788266217001"}},
		{OperationSealStage, []string{"SEALED", now, digest, "14680064", "4"}},
		{OperationFinalizeRun, []string{"NOT_DUE", now, now, "none"}},
	}
	for _, test := range invalidRelations {
		if err := ValidateOperationResponse(test.operation, test.response); !errors.Is(err, ErrResponseScalar) {
			t.Fatalf("%s invalid relation error = %v", test.operation, err)
		}
	}
	// Near stage expiry, the durable backpressure deadline can already precede
	// the first blocked timestamp and remains a valid immediate-abort result.
	if err := ValidateOperationResponse(OperationCommit, []string{"DOWNSTREAM_BACKPRESSURE", now, "pages_queue_full", now, "1788266096999"}); err != nil {
		t.Fatalf("valid elapsed commit deadline: %v", err)
	}
}

func TestRenewLeaseResponseUsesTypedStageCapContext(t *testing.T) {
	const (
		firstNow       = uint64(1_788_266_097_000)
		secondNow      = firstNow + 1_000
		stageExpiresAt = firstNow + 45_000
	)
	noncapped := []string{
		string(StatusRenewed), canonicalDecimal(firstNow), canonicalDecimal(firstNow + LeaseTTLMilliseconds),
	}
	capped := []string{
		string(StatusRenewed), canonicalDecimal(firstNow), canonicalDecimal(stageExpiresAt),
	}

	if err := ValidateOperationResponse(OperationRenewLease, noncapped); !errors.Is(err, ErrResponseContextRequired) {
		t.Fatalf("generic non-capped renew error = %v", err)
	}
	if err := ValidateOperationResponse(OperationRenewLease, capped); !errors.Is(err, ErrResponseContextRequired) {
		t.Fatalf("generic capped renew error = %v", err)
	}
	if err := ValidateRenewLeaseResponse(NewUnstagedRenewLeaseResponseContext(), noncapped); err != nil {
		t.Fatalf("unstaged renewal: %v", err)
	}
	if err := ValidateRenewLeaseResponse(NewUnstagedRenewLeaseResponseContext(), capped); !errors.Is(err, ErrResponseScalar) {
		t.Fatalf("unstaged context accepted stage cap: %v", err)
	}

	stageContext, err := NewStagedRenewLeaseResponseContext(RedisMilliseconds(stageExpiresAt))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateRenewLeaseResponse(stageContext, capped); err != nil {
		t.Fatalf("exact stage-capped renewal: %v", err)
	}
	for _, expiry := range []uint64{stageExpiresAt - 1, stageExpiresAt + 1, firstNow + LeaseTTLMilliseconds} {
		response := []string{string(StatusRenewed), canonicalDecimal(firstNow), canonicalDecimal(expiry)}
		if err := ValidateRenewLeaseResponse(stageContext, response); !errors.Is(err, ErrResponseScalar) {
			t.Fatalf("inexact stage cap %d error = %v", expiry, err)
		}
	}

	// A replay before the absolute stage expiry returns the same cap even though
	// Redis TIME advanced; it remains an exact min(now+TTL, stage expiry).
	replay := []string{string(StatusRenewed), canonicalDecimal(secondNow), canonicalDecimal(stageExpiresAt)}
	if err := ValidateRenewLeaseResponse(stageContext, replay); err != nil {
		t.Fatalf("stage-capped replay: %v", err)
	}

	lateStageContext, err := NewStagedRenewLeaseResponseContext(RedisMilliseconds(firstNow + 120_000))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateRenewLeaseResponse(lateStageContext, noncapped); err != nil {
		t.Fatalf("non-capped staged renewal: %v", err)
	}
	if err := ValidateRenewLeaseResponse(RenewLeaseResponseContext{}, noncapped); !errors.Is(err, ErrInvalidRenewLeaseContext) {
		t.Fatalf("zero renew context error = %v", err)
	}
}

func TestOperationResponsesRejectMalformedDataWithoutDisclosure(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	intent := vectorReservationValue(t, fixture, false)
	runPolicy := vectorRunPolicyAuthority(t, fixture, plainSHA256(testDenyAllRenderPolicyArtifact()))
	valid := []string{"STARTED", "1788266097000", fixture.Expected.ReservationID, "1788266097000", "1", "1", "1", "1", "1"}
	tests := []struct {
		name string
		raw  any
		want error
	}{
		{name: "not array", raw: "STARTED", want: ErrResponseNotArray},
		{name: "too short", raw: []string{"STARTED"}, want: ErrResponseArity},
		{name: "too long", raw: append(append([]string(nil), valid...), "extra"), want: ErrResponseArity},
		{name: "integer scalar", raw: []any{"STARTED", int64(1)}, want: ErrResponseScalarType},
		{name: "unknown status", raw: []string{"RAW_RESPONSE_CANARY", "1"}, want: ErrUnknownStatus},
		{name: "noncanonical time", raw: []string{"RUN_CANCELLED", "01"}, want: ErrInvalidRedisMillis},
		{name: "bad reservation", raw: []string{"STARTED", "1788266097000", "RESERVATION_CANARY", "1788266096789", "1", "1", "1", "1", "1"}, want: ErrResponseScalar},
		{name: "started without permission", raw: []string{"STARTED", "1788266097000", fixture.Expected.ReservationID, "1788266096789", "1", "1", "1", "1", "0"}, want: ErrResponseScalar},
		{name: "future start", raw: []string{"STARTED", "1788266096000", fixture.Expected.ReservationID, "1788266096789", "1", "1", "1", "1", "1"}, want: ErrResponseScalar},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newTestTransportAuthority().parseStartRequestResponse(runPolicy, intent, test.raw)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if strings.Contains(err.Error(), "CANARY") {
				t.Fatalf("response error exposed raw input: %v", err)
			}
		})
	}
}

func TestTypedStageChunkConstructorsCoverAllKinds(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	context := vectorOutputContextValue(t, fixture)
	output := vectorOutputValue(t, fixture)
	commitID := Digest(fixture.Expected.CommitID)
	publicationID := Digest(fixture.Expected.PublicationID)
	identity := outputCommitIdentity(context, publicationID)

	pageFields, err := NewPageFieldsStageChunk(identity, context, output.Page)
	if err != nil {
		t.Fatal(err)
	}
	html, err := NewPageBlobStageChunk(identity, context, ChunkHTML, output.Page.HTML)
	if err != nil {
		t.Fatal(err)
	}
	originalHTML, err := NewPageBlobStageChunk(identity, context, ChunkOriginalHTML, output.Page.OriginalHTML)
	if err != nil {
		t.Fatal(err)
	}
	outlinks, err := NewOutlinksStageChunk(identity, 0, context, output.Outlinks)
	if err != nil {
		t.Fatal(err)
	}
	discoveries, err := NewDiscoveriesStageChunk(identity, 0, context, output.Discoveries)
	if err != nil {
		t.Fatal(err)
	}
	aliases, err := NewAliasesStageChunk(identity, context)
	if err != nil {
		t.Fatal(err)
	}
	images, err := NewImagesStageChunk(identity, context, output.Images)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := NewImageManifestStageChunk(identity, context, output.Images)
	if err != nil {
		t.Fatal(err)
	}

	chunks := []StageChunk{pageFields, html, originalHTML, outlinks, discoveries, aliases, images, manifest}
	wantKinds := []ChunkKind{
		ChunkPageFields, ChunkHTML, ChunkOriginalHTML, ChunkOutlinks,
		ChunkDiscoveries, ChunkAliases, ChunkImages, ChunkImageManifest,
	}
	for index, chunk := range chunks {
		if chunk.Kind() != wantKinds[index] || chunk.CommitID() != commitID {
			t.Fatalf("chunk %d identity mismatch", index)
		}
		if _, err := DeriveChunkDigest(chunk); err != nil {
			t.Fatalf("chunk %q: %v", chunk.Kind(), err)
		}
	}
	if _, err := DeriveChunkDigest(StageChunk{}); err == nil {
		t.Fatal("zero/unvalidated chunk was accepted")
	}
	if _, err := NewPageBlobStageChunk(identity, context, ChunkOutlinks, nil); !errors.Is(err, ErrUnknownChunkKind) {
		t.Fatalf("blob kind error = %v", err)
	}
	if _, err := NewOutlinksStageChunk(identity, MaxOutlinkChunks, context, []string{"https://example.com/a"}); !errors.Is(err, ErrInvalidChunk) {
		t.Fatalf("outlink ordinal error = %v", err)
	}
}
