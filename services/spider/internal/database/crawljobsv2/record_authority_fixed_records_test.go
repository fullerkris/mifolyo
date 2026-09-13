package crawljobsv2

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestRecordAuthorityJobValidationAcceptsEveryState(t *testing.T) {
	if jobLeaseRequestStartsBaselineIndex != 16 || jobLeaseDeliveryStartedIndex != 33 || jobActiveReservationIDIndex != 34 {
		t.Fatalf("job index constants drifted: lease_delivery_started=%d active_reservation_id=%d", jobLeaseDeliveryStartedIndex, jobActiveReservationIDIndex)
	}
	states := []string{"ready", "leased", "delayed", "completed", "dead", "cancelled"}
	for _, state := range states {
		t.Run(state, func(t *testing.T) {
			record := recordAuthorityJobRecord(t, state)
			if err := ValidateRecord(SchemaJob, record); err != nil {
				t.Fatalf("valid %s job rejected: %v", state, err)
			}
		})
	}

	preIO := recordAuthorityJobRecord(t, "leased")
	recordAuthoritySet(preIO, jobDeliveryAttemptsIndex, "0")
	recordAuthoritySet(preIO, jobRequestStartsIndex, "0")
	recordAuthoritySet(preIO, jobLastRequestStartedAtMSIndex, "0")
	recordAuthoritySet(preIO, jobLeaseDeliveryStartedIndex, "0")
	if err := ValidateRecord(SchemaJob, preIO); err != nil {
		t.Fatalf("valid pre-I/O leased job rejected: %v", err)
	}
}

func TestRecordAuthorityJobOneFieldMutationsAreRejected(t *testing.T) {
	mutations := []struct {
		name  string
		state string
		index int
		value string
	}{
		{name: "ready active reservation", state: "ready", index: jobActiveReservationIDIndex, value: strings.Repeat("e", 64)},
		{name: "leased delivery sentinel", state: "leased", index: jobLeaseDeliveryStartedIndex, value: "2"},
		{name: "leased active reservation grammar", state: "leased", index: jobActiveReservationIDIndex, value: "1"},
		{name: "delayed deadline", state: "delayed", index: jobNotBeforeMSIndex, value: "0"},
		{name: "completed timestamp", state: "completed", index: jobCompletedAtMSIndex, value: "0"},
		{name: "dead timestamp", state: "dead", index: jobDeadAtMSIndex, value: "0"},
		{name: "cancelled timestamp", state: "cancelled", index: jobCancelledAtMSIndex, value: "0"},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			record := recordAuthorityJobRecord(t, mutation.state)
			recordAuthoritySet(record, mutation.index, mutation.value)
			if err := ValidateRecord(SchemaJob, record); err == nil {
				t.Fatal("one-field mutation was accepted")
			}
		})
	}
}

func TestRecordAuthorityJobAcceptsElapsedCommitBackpressureDeadline(t *testing.T) {
	record := recordAuthorityJobRecord(t, "leased")
	commitID := strings.Repeat("6", 64)
	recordAuthoritySet(record, jobActiveReservationIDIndex, "")
	recordAuthoritySet(record, jobActiveStageCommitIDIndex, commitID)
	recordAuthoritySet(record, jobLastStageCommitIDIndex, commitID)
	recordAuthoritySet(record, jobLastStageFenceIndex, "1")
	recordAuthoritySet(record, jobCommitBackpressureFenceIndex, "1")
	recordAuthoritySet(record, jobCommitBackpressureReasonIndex, "pages_queue_full")
	recordAuthoritySet(record, jobCommitBackpressureStartedAtMSIndex, "150")
	recordAuthoritySet(record, jobCommitBackpressureDeadlineMSIndex, "149")

	if err := ValidateRecord(SchemaJob, record); err != nil {
		t.Fatalf("valid elapsed commit-backpressure deadline rejected: %v", err)
	}

	atMaximum := cloneRecord(record)
	recordAuthoritySet(atMaximum, jobCommitBackpressureDeadlineMSIndex, "120150")
	if err := ValidateRecord(SchemaJob, atMaximum); err != nil {
		t.Fatalf("maximum commit-backpressure deadline rejected: %v", err)
	}

	mutations := []struct {
		name     string
		deadline string
	}{
		{name: "missing deadline", deadline: "0"},
		{name: "deadline above maximum", deadline: "120151"},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			mutated := cloneRecord(record)
			recordAuthoritySet(mutated, jobCommitBackpressureDeadlineMSIndex, mutation.deadline)
			if err := ValidateRecord(SchemaJob, mutated); !errors.Is(err, ErrRecordRelation) {
				t.Fatalf("commit-backpressure deadline error = %v", err)
			}
		})
	}
}

func TestRecordAuthorityCompletedJobPublicationEvidence(t *testing.T) {
	published := recordAuthorityPublishedJobRecord(t)
	if err := ValidateRecord(SchemaJob, published); err != nil {
		t.Fatalf("valid published completion rejected: %v", err)
	}
	noOutput := recordAuthorityJobRecord(t, "completed")
	if err := ValidateRecord(SchemaJob, noOutput); err != nil {
		t.Fatalf("valid no-output completion rejected: %v", err)
	}

	wrongPageKey, err := PageDataKey(Digest(string(published[jobPublicationIDIndex].Value)), "https://example.com/path")
	if err != nil {
		t.Fatal(err)
	}
	fenceOnePublicationID, err := DerivePublicationID(PublicationIdentity{
		RunID:        RunID(string(published[jobRunIDIndex].Value)),
		JobID:        JobID(string(published[jobJobIDIndex].Value)),
		Fence:        Fence(1),
		OutputDigest: Digest(string(published[jobOutputDigestIndex].Value)),
	})
	if err != nil {
		t.Fatal(err)
	}
	fenceOnePageKey, err := PageDataKey(fenceOnePublicationID, string(published[jobLastDocumentTargetURLIndex].Value))
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name   string
		base   Record
		mutate func(Record)
	}{
		{name: "missing output digest", base: published, mutate: func(record Record) {
			recordAuthoritySet(record, jobOutputDigestIndex, "")
		}},
		{name: "missing publication ID", base: published, mutate: func(record Record) {
			recordAuthoritySet(record, jobPublicationIDIndex, "")
		}},
		{name: "missing commit ID", base: published, mutate: func(record Record) {
			recordAuthoritySet(record, jobCommitIDIndex, "")
		}},
		{name: "missing published page key", base: published, mutate: func(record Record) {
			recordAuthoritySet(record, jobPublishedPageKeyIndex, "")
		}},
		{name: "unrelated output digest", base: published, mutate: func(record Record) {
			recordAuthoritySet(record, jobOutputDigestIndex, strings.Repeat("8", 64))
		}},
		{name: "unrelated publication ID", base: published, mutate: func(record Record) {
			recordAuthoritySet(record, jobPublicationIDIndex, strings.Repeat("9", 64))
		}},
		{name: "commit differs from last stage", base: published, mutate: func(record Record) {
			recordAuthoritySet(record, jobCommitIDIndex, strings.Repeat("9", 64))
		}},
		{name: "missing document lineage", base: published, mutate: func(record Record) {
			for _, index := range []int{
				jobLastDocumentRequestStartedAtMSIndex, jobLastDocumentRequestFenceIndex,
				jobLastDocumentTargetURLIDIndex, jobLastDocumentTargetURLIndex, jobLastDocumentTargetDigestIndex,
			} {
				value := ""
				if index == jobLastDocumentRequestStartedAtMSIndex || index == jobLastDocumentRequestFenceIndex {
					value = "0"
				}
				recordAuthoritySet(record, index, value)
			}
		}},
		{name: "document fence differs from stage fence", base: published, mutate: func(record Record) {
			recordAuthoritySet(record, jobLastDocumentRequestFenceIndex, "1")
		}},
		{name: "stage fence differs from lease and document fences", base: published, mutate: func(record Record) {
			recordAuthoritySet(record, jobLastStageFenceIndex, "1")
			recordAuthoritySet(record, jobPublicationIDIndex, string(fenceOnePublicationID))
			recordAuthoritySet(record, jobPublishedPageKeyIndex, fenceOnePageKey)
		}},
		{name: "published completion reason is no-output", base: published, mutate: func(record Record) {
			recordAuthoritySet(record, jobLastReasonIndex, string(ReasonAlreadyVisited))
		}},
		{name: "published page key uses original URL", base: published, mutate: func(record Record) {
			recordAuthoritySet(record, jobPublishedPageKeyIndex, wrongPageKey)
		}},
		{name: "no-output completion reason is published", base: noOutput, mutate: func(record Record) {
			recordAuthoritySet(record, jobLastReasonIndex, string(ReasonPublished))
		}},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			record := cloneRecord(mutation.base)
			mutation.mutate(record)
			if err := ValidateRecord(SchemaJob, record); !errors.Is(err, ErrRecordRelation) {
				t.Fatalf("publication evidence mutation error = %v", err)
			}
		})
	}
}

func TestRecordAuthorityAbsoluteCounterBounds(t *testing.T) {
	jobStartsAtLimit := recordAuthorityJobRecord(t, "leased")
	recordAuthoritySet(jobStartsAtLimit, jobRequestStartsIndex, canonicalDecimal(MaxRequestStartsPerRun))
	recordAuthoritySet(jobStartsAtLimit, jobNextRequestOrdinalIndex, canonicalDecimal(MaxRequestStartsPerRun+1))

	jobOrdinalAtLimit := recordAuthorityJobRecord(t, "ready")
	recordAuthoritySet(jobOrdinalAtLimit, jobNextRequestOrdinalIndex, canonicalDecimal(MaxReservationCreationsPerRun+1))

	reservationOrdinalAtLimit := recordAuthorityReservationRecord(t, "pending")
	recordAuthoritySet(reservationOrdinalAtLimit, reservationRequestOrdinalIndex, canonicalDecimal(MaxReservationCreationsPerRun))
	reservationValues := make([]string, len(reservationOrdinalAtLimit))
	for index := range reservationOrdinalAtLimit {
		reservationValues[index] = string(reservationOrdinalAtLimit[index].Value)
	}
	recordAuthoritySet(reservationOrdinalAtLimit, reservationIDIndex, string(deriveReservationRecordID(reservationValues)))

	tests := []struct {
		name   string
		schema RecordSchema
		valid  Record
		mutate func(Record)
	}{
		{name: "job request starts", schema: SchemaJob, valid: jobStartsAtLimit, mutate: func(record Record) {
			recordAuthoritySet(record, jobRequestStartsIndex, canonicalDecimal(MaxRequestStartsPerRun+1))
			recordAuthoritySet(record, jobNextRequestOrdinalIndex, canonicalDecimal(MaxRequestStartsPerRun+2))
		}},
		{name: "job next request ordinal", schema: SchemaJob, valid: jobOrdinalAtLimit, mutate: func(record Record) {
			recordAuthoritySet(record, jobNextRequestOrdinalIndex, canonicalDecimal(MaxReservationCreationsPerRun+2))
		}},
		{name: "reservation request ordinal", schema: SchemaReservation, valid: reservationOrdinalAtLimit, mutate: func(record Record) {
			recordAuthoritySet(record, reservationRequestOrdinalIndex, canonicalDecimal(MaxReservationCreationsPerRun+1))
			values := make([]string, len(record))
			for index := range record {
				values[index] = string(record[index].Value)
			}
			recordAuthoritySet(record, reservationIDIndex, string(deriveReservationRecordID(values)))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateRecord(test.schema, test.valid); err != nil {
				t.Fatalf("maximum valid value rejected: %v", err)
			}
			mutated := cloneRecord(test.valid)
			test.mutate(mutated)
			if err := ValidateRecord(test.schema, mutated); !errors.Is(err, ErrRecordRelation) {
				t.Fatalf("above-maximum mutation error = %v", err)
			}
		})
	}
}

func TestRecordAuthorityRunValidationAcceptsEveryLifecycleState(t *testing.T) {
	states := []string{"loading", "auditing", "sealed", "active", "completed", "budget_exhausted", "cancelled", "archived"}
	for _, state := range states {
		t.Run(state, func(t *testing.T) {
			record := recordAuthorityRunRecord(t, state)
			if err := ValidateRecord(SchemaRun, record); err != nil {
				t.Fatalf("valid %s run rejected: %v", state, err)
			}
		})
	}

	active := recordAuthorityRunRecord(t, "active")
	if jobCount, seedCount := string(active[runJobCountIndex].Value), string(active[runExpectedSeedCountIndex].Value); jobCount != "3" || seedCount != "2" {
		t.Fatalf("active discovery counts = job_count %s, expected_seed_count %s", jobCount, seedCount)
	}

	drainingCancellation := recordAuthorityRunRecord(t, "cancelled")
	recordAuthoritySet(drainingCancellation, runOpenJobCountIndex, "1")
	recordAuthoritySet(drainingCancellation, runCancelledTotalIndex, "0")
	recordAuthoritySet(drainingCancellation, runFinalizedAtMSIndex, "0")
	recordAuthoritySet(drainingCancellation, runRetentionAnchorMSIndex, "0")
	recordAuthoritySet(drainingCancellation, runLastActivityAtMSIndex, "575")
	recordAuthoritySet(drainingCancellation, runLastTerminalTransitionAtMSIndex, "500")
	if err := ValidateRecord(SchemaRun, drainingCancellation); err != nil {
		t.Fatalf("valid draining cancelled run rejected: %v", err)
	}
	for _, predecessor := range []struct {
		state       string
		cancelledAt string
	}{
		{state: "loading", cancelledAt: "200"},
		{state: "auditing", cancelledAt: "250"},
		{state: "sealed", cancelledAt: "350"},
	} {
		t.Run("cancelled from "+predecessor.state, func(t *testing.T) {
			record := recordAuthorityRunRecord(t, predecessor.state)
			recordAuthoritySet(record, runStateIndex, "cancelled")
			recordAuthoritySet(record, runCancelledAtMSIndex, predecessor.cancelledAt)
			recordAuthoritySet(record, runLastActivityAtMSIndex, predecessor.cancelledAt)
			recordAuthoritySet(record, runTerminalReasonIndex, "operator_cancelled")
			if err := ValidateRecord(SchemaRun, record); err != nil {
				t.Fatalf("valid cancellation from %s rejected: %v", predecessor.state, err)
			}
		})
	}

	for _, budget := range []struct {
		name                 string
		requestStarts        string
		reservationCreations string
		reason               string
	}{
		{name: "reservation limit", requestStarts: "9", reservationCreations: "100", reason: "reservation_limit_exhausted"},
		{name: "group limits", requestStarts: "9", reservationCreations: "9", reason: "group_budgets_exhausted"},
	} {
		t.Run("budget "+budget.name, func(t *testing.T) {
			record := recordAuthorityRunRecord(t, "budget_exhausted")
			recordAuthoritySet(record, runRequestStartsIndex, budget.requestStarts)
			recordAuthoritySet(record, runReservationCreationsTotalIndex, budget.reservationCreations)
			recordAuthoritySet(record, runTerminalReasonIndex, budget.reason)
			if err := ValidateRecord(SchemaRun, record); err != nil {
				t.Fatalf("valid %s run rejected: %v", budget.name, err)
			}
		})
	}

	for _, predecessor := range []string{"budget_exhausted", "cancelled"} {
		t.Run("archived from "+predecessor, func(t *testing.T) {
			record := recordAuthorityRunRecord(t, predecessor)
			recordAuthoritySet(record, runStateIndex, "archived")
			recordAuthoritySet(record, runArchivedAtMSIndex, "1000")
			recordAuthoritySet(record, runArchiveSHA256Index, strings.Repeat("b", 64))
			if err := ValidateRecord(SchemaRun, record); err != nil {
				t.Fatalf("valid archived %s run rejected: %v", predecessor, err)
			}
		})
	}
}

func TestRecordAuthorityRunLifecycleOneFieldMutationsAreRejected(t *testing.T) {
	mutations := []struct {
		name  string
		state string
		index int
		value string
	}{
		{name: "loading source count", state: "loading", index: runExpectedSeedCountIndex, value: "1"},
		{name: "auditing frozen revision", state: "auditing", index: runAuditRevisionIndex, value: "0"},
		{name: "sealed audit completion", state: "sealed", index: runAuditCompleteIndex, value: "0"},
		{name: "active activation timestamp", state: "active", index: runActivatedAtMSIndex, value: "0"},
		{name: "completed open jobs", state: "completed", index: runOpenJobCountIndex, value: "1"},
		{name: "completed terminal counter", state: "completed", index: runCompletedTotalIndex, value: "1"},
		{name: "completed timestamp", state: "completed", index: runCompletedAtMSIndex, value: "0"},
		{name: "budget reason", state: "budget_exhausted", index: runTerminalReasonIndex, value: "all_jobs_terminal"},
		{name: "budget reason precedence", state: "budget_exhausted", index: runTerminalReasonIndex, value: "reservation_limit_exhausted"},
		{name: "budget timestamp", state: "budget_exhausted", index: runBudgetExhaustedAtMSIndex, value: "0"},
		{name: "cancelled timestamp", state: "cancelled", index: runCancelledAtMSIndex, value: "0"},
		{name: "cancelled terminal counter", state: "cancelled", index: runCancelledTotalIndex, value: "0"},
		{name: "cancelled reason", state: "cancelled", index: runTerminalReasonIndex, value: "all_jobs_terminal"},
		{name: "archived evidence", state: "archived", index: runArchiveSHA256Index, value: ""},
		{name: "archived timestamp", state: "archived", index: runArchivedAtMSIndex, value: "0"},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			record := recordAuthorityRunRecord(t, mutation.state)
			recordAuthoritySet(record, mutation.index, mutation.value)
			if err := ValidateRecord(SchemaRun, record); !errors.Is(err, ErrRecordRelation) {
				t.Fatalf("one-field mutation error = %v", err)
			}
		})
	}
}

func TestRecordAuthorityReservationIdentityLifecycleAndCounters(t *testing.T) {
	for _, state := range []string{"pending", "started", "finished", "cancelled", "expired"} {
		t.Run(state, func(t *testing.T) {
			record := recordAuthorityReservationRecord(t, state)
			if err := ValidateRecord(SchemaReservation, record); err != nil {
				t.Fatalf("valid %s reservation rejected: %v", state, err)
			}
		})
	}

	mutations := []struct {
		name  string
		state string
		index int
		value string
	}{
		{name: "reservation identity", state: "pending", index: reservationIDIndex, value: strings.Repeat("f", 64)},
		{name: "target digest", state: "pending", index: reservationTargetDigestIndex, value: strings.Repeat("f", 64)},
		{name: "policy decision digest", state: "pending", index: reservationPolicyDecisionSHA256Index, value: strings.Repeat("f", 64)},
		{name: "global scope", state: "pending", index: reservationGlobalScopeIDIndex, value: strings.Repeat("f", 64)},
		{name: "group scope", state: "pending", index: reservationGroupScopeIDIndex, value: strings.Repeat("f", 64)},
		{name: "origin scope", state: "pending", index: reservationOriginScopeIDIndex, value: strings.Repeat("f", 64)},
		{name: "unequal tuple", state: "pending", index: reservationOriginConcurrencyIndex, value: "2"},
		{name: "pending snapshot", state: "pending", index: reservationJobStartsAfterStartIndex, value: "1"},
		{name: "started terminal time", state: "started", index: reservationTerminalAtMSIndex, value: "300"},
		{name: "counter ordering", state: "started", index: reservationDeliveryAttemptsAfterStartIndex, value: "2"},
		{name: "finished start time", state: "finished", index: reservationStartedAtMSIndex, value: "0"},
		{name: "cancelled start time", state: "cancelled", index: reservationStartedAtMSIndex, value: "200"},
		{name: "expired terminal time", state: "expired", index: reservationTerminalAtMSIndex, value: "999"},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			record := recordAuthorityReservationRecord(t, mutation.state)
			recordAuthoritySet(record, mutation.index, mutation.value)
			if err := ValidateRecord(SchemaReservation, record); err == nil {
				t.Fatal("one-field mutation was accepted")
			}
		})
	}
}

func TestRecordAuthorityStageMetaRequiresExactTTLAndPublicationIdentity(t *testing.T) {
	valid := recordAuthorityUnsealedStageRecord(t, "0")
	if err := ValidateRecord(SchemaStageMeta, valid); err != nil {
		t.Fatalf("valid stage metadata rejected: %v", err)
	}
	boundary := cloneRecord(valid)
	recordAuthoritySet(boundary, stageCreatedAtMSIndex, canonicalDecimal(MaxExactInteger-StageTTLMilliseconds))
	recordAuthoritySet(boundary, stageExpiresAtMSIndex, canonicalDecimal(MaxExactInteger))
	if err := ValidateRecord(SchemaStageMeta, boundary); err != nil {
		t.Fatalf("latest non-overflowing stage TTL rejected: %v", err)
	}

	mutations := []struct {
		name   string
		mutate func(Record)
	}{
		{name: "short TTL", mutate: func(record Record) {
			recordAuthoritySet(record, stageExpiresAtMSIndex, canonicalDecimal(100+StageTTLMilliseconds-1))
		}},
		{name: "long TTL", mutate: func(record Record) {
			recordAuthoritySet(record, stageExpiresAtMSIndex, canonicalDecimal(100+StageTTLMilliseconds+1))
		}},
		{name: "overflowing TTL", mutate: func(record Record) {
			recordAuthoritySet(record, stageCreatedAtMSIndex, canonicalDecimal(MaxExactInteger-StageTTLMilliseconds+1))
			recordAuthoritySet(record, stageExpiresAtMSIndex, canonicalDecimal(MaxExactInteger))
		}},
		{name: "unrelated run identity", mutate: func(record Record) {
			recordAuthoritySet(record, stageRunIDIndex, strings.Repeat("8", 32))
		}},
		{name: "unrelated job identity", mutate: func(record Record) {
			recordAuthoritySet(record, stageJobIDIndex, strings.Repeat("8", 64))
		}},
		{name: "unrelated fence identity", mutate: func(record Record) {
			recordAuthoritySet(record, stageLeaseFenceIndex, "2")
		}},
		{name: "unrelated output identity", mutate: func(record Record) {
			recordAuthoritySet(record, stageOutputDigestIndex, strings.Repeat("8", 64))
		}},
		{name: "unrelated publication identity", mutate: func(record Record) {
			recordAuthoritySet(record, stagePublicationIDIndex, strings.Repeat("8", 64))
		}},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			record := cloneRecord(valid)
			mutation.mutate(record)
			if err := ValidateRecord(SchemaStageMeta, record); !errors.Is(err, ErrRecordRelation) {
				t.Fatalf("stage metadata mutation error = %v", err)
			}
		})
	}
}

func TestRecordAuthorityAdditionalLocalRelations(t *testing.T) {
	t.Run("run claims do not exceed reservation creations", func(t *testing.T) {
		record := recordAuthorityRunRecord(t, "loading")
		if err := ValidateRecord(SchemaRun, record); err != nil {
			t.Fatalf("valid run rejected: %v", err)
		}
		recordAuthoritySet(record, runClaimsTotalIndex, "1")
		if err := ValidateRecord(SchemaRun, record); !errors.Is(err, ErrRecordRelation) {
			t.Fatalf("claims relation error = %v", err)
		}
	})

	t.Run("rate deadline is conservative", func(t *testing.T) {
		record := recordAuthorityRateScopeRecord(t)
		if err := ValidateRecord(SchemaRateScope, record); err != nil {
			t.Fatalf("valid rate scope rejected: %v", err)
		}
		recordAuthoritySet(record, rateScopeNextAllowedMSIndex, "399")
		if err := ValidateRecord(SchemaRateScope, record); !errors.Is(err, ErrRecordRelation) {
			t.Fatalf("deadline relation error = %v", err)
		}
	})

	t.Run("sealed stage is complete", func(t *testing.T) {
		record := recordAuthoritySealedStageRecord(t, "0")
		if err := ValidateRecord(SchemaStageMeta, record); err != nil {
			t.Fatalf("valid sealed stage rejected: %v", err)
		}
		recordAuthoritySet(record, stageManifestChunkDigestIndex, "")
		if err := ValidateRecord(SchemaStageMeta, record); !errors.Is(err, ErrRecordRelation) {
			t.Fatalf("sealed completeness error = %v", err)
		}
	})

	t.Run("recovery may abandon sealed or unsealed state", func(t *testing.T) {
		sealed := recordAuthoritySealedStageRecord(t, "1")
		if err := ValidateRecord(SchemaStageMeta, sealed); err != nil {
			t.Fatalf("valid sealed abandoned stage rejected: %v", err)
		}
		unsealed := recordAuthorityUnsealedStageRecord(t, "1")
		if err := ValidateRecord(SchemaStageMeta, unsealed); err != nil {
			t.Fatalf("valid unsealed abandoned stage rejected: %v", err)
		}
		recordAuthoritySet(unsealed, stageKeyCountIndex, "3")
		if err := ValidateRecord(SchemaStageMeta, unsealed); !errors.Is(err, ErrRecordRelation) {
			t.Fatalf("abandoned key-count relation error = %v", err)
		}
	})
}

func recordAuthorityJobRecord(t *testing.T, state string) Record {
	t.Helper()
	canonicalURL := "https://example.com/path"
	jobID := recordAuthorityURLID(canonicalURL)
	rateScopeID := RateScopeID(strings.Repeat("2", 32))
	groupScopeID, err := DeriveGroupScopeID(rateScopeID)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := DeriveCanonicalOrigin(canonicalURL)
	if err != nil {
		t.Fatal(err)
	}
	originScopeID, err := DeriveOriginScopeID(origin)
	if err != nil {
		t.Fatal(err)
	}

	values := make([]string, jobCancelledAtMSIndex+1)
	for _, index := range []int{
		jobDepthIndex, jobClaimCountIndex, jobDeliveryAttemptsIndex, jobRequestStartsIndex,
		jobLeaseRequestStartsBaselineIndex,
		jobRetryCountIndex, jobPreIORecoveriesIndex, jobNextRequestOrdinalIndex,
		jobLastRequestStartedAtMSIndex, jobLastDocumentRequestStartedAtMSIndex,
		jobLastDocumentRequestFenceIndex, jobLeaseFenceIndex, jobLeaseStartedAtMSIndex,
		jobLeaseExpiresAtMSIndex, jobLeaseDeliveryStartedIndex, jobLastStageFenceIndex,
		jobNotBeforeMSIndex, jobCommitBackpressureFenceIndex, jobCommitBackpressureStartedAtMSIndex,
		jobCommitBackpressureDeadlineMSIndex, jobCreatedAtMSIndex, jobUpdatedAtMSIndex,
		jobCompletedAtMSIndex, jobDeadAtMSIndex, jobCancelledAtMSIndex,
	} {
		values[index] = "0"
	}
	values[jobProtocolVersionIndex] = "2"
	values[jobRunIDIndex] = strings.Repeat("1", 32)
	values[jobJobIDIndex] = string(jobID)
	values[jobURLIDIndex] = string(jobID)
	values[jobCanonicalURLIndex] = canonicalURL
	values[jobScoreTextIndex] = "0"
	values[jobStateIndex] = state
	values[jobGroupIDIndex] = "default"
	values[jobRateScopeIDIndex] = string(rateScopeID)
	values[jobGroupScopeIDIndex] = string(groupScopeID)
	values[jobInitialOriginScopeIDIndex] = string(originScopeID)
	values[jobPolicyDecisionSHA256Index] = strings.Repeat("a", 64)
	values[jobNextRequestOrdinalIndex] = "1"
	values[jobLastReasonIndex] = "none"
	values[jobLastFailureReasonIndex] = "none"
	values[jobCommitBackpressureReasonIndex] = "none"
	values[jobCreatedAtMSIndex] = "100"
	values[jobUpdatedAtMSIndex] = "100"

	switch state {
	case "leased":
		values[jobClaimCountIndex] = "1"
		values[jobDeliveryAttemptsIndex] = "1"
		values[jobRequestStartsIndex] = "1"
		values[jobNextRequestOrdinalIndex] = "2"
		values[jobLastRequestStartedAtMSIndex] = "150"
		values[jobLeaseOwnerIndex] = strings.Repeat("3", 32)
		values[jobLeaseTokenIndex] = strings.Repeat("4", 64)
		values[jobLeaseFenceIndex] = "1"
		values[jobLeaseStartedAtMSIndex] = "100"
		values[jobLeaseExpiresAtMSIndex] = "1000"
		values[jobLeaseDeliveryStartedIndex] = "1"
		values[jobActiveReservationIDIndex] = strings.Repeat("5", 64)
		values[jobUpdatedAtMSIndex] = "150"
	case "delayed":
		values[jobClaimCountIndex] = "1"
		values[jobDeliveryAttemptsIndex] = "1"
		values[jobRequestStartsIndex] = "1"
		values[jobRetryCountIndex] = "1"
		values[jobNextRequestOrdinalIndex] = "2"
		values[jobLastRequestStartedAtMSIndex] = "100"
		values[jobLeaseFenceIndex] = "1"
		values[jobNotBeforeMSIndex] = "200"
	case "completed":
		values[jobLastReasonIndex] = "already_visited"
		values[jobCompletedAtMSIndex] = "100"
	case "dead":
		values[jobLastReasonIndex] = "policy_denied"
		values[jobLastFailureReasonIndex] = "policy_denied"
		values[jobDeadAtMSIndex] = "100"
	case "cancelled":
		values[jobLastReasonIndex] = "operator_cancelled"
		values[jobCancelledAtMSIndex] = "100"
	}
	return recordAuthorityRecord(t, SchemaJob, values)
}

func recordAuthorityPublishedJobRecord(t *testing.T) Record {
	t.Helper()
	record := recordAuthorityJobRecord(t, "completed")
	finalURL := "https://example.com/final"
	finalURLID := recordAuthorityURLID(finalURL)
	finalTargetDigest, err := DeriveTargetDigest(RequestTarget{URLID: finalURLID, CanonicalURL: finalURL})
	if err != nil {
		t.Fatal(err)
	}
	outputDigest := Digest(strings.Repeat("7", 64))
	publicationID, err := DerivePublicationID(PublicationIdentity{
		RunID: RunID(string(record[jobRunIDIndex].Value)), JobID: JobID(string(record[jobJobIDIndex].Value)),
		Fence: Fence(2), OutputDigest: outputDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	publishedPageKey, err := PageDataKey(publicationID, finalURL)
	if err != nil {
		t.Fatal(err)
	}
	stageCommitID := strings.Repeat("6", 64)
	for _, change := range []struct {
		index int
		value string
	}{
		{jobClaimCountIndex, "2"},
		{jobDeliveryAttemptsIndex, "1"},
		{jobRequestStartsIndex, "1"},
		{jobPreIORecoveriesIndex, "1"},
		{jobNextRequestOrdinalIndex, "3"},
		{jobLastRequestStartedAtMSIndex, "150"},
		{jobLastDocumentRequestStartedAtMSIndex, "150"},
		{jobLastDocumentRequestFenceIndex, "2"},
		{jobLastDocumentTargetURLIDIndex, string(finalURLID)},
		{jobLastDocumentTargetURLIndex, finalURL},
		{jobLastDocumentTargetDigestIndex, string(finalTargetDigest)},
		{jobLastReasonIndex, string(ReasonPublished)},
		{jobLeaseFenceIndex, "2"},
		{jobLastStageCommitIDIndex, stageCommitID},
		{jobLastStageFenceIndex, "2"},
		{jobOutputDigestIndex, string(outputDigest)},
		{jobPublicationIDIndex, string(publicationID)},
		{jobCommitIDIndex, stageCommitID},
		{jobPublishedPageKeyIndex, publishedPageKey},
		{jobUpdatedAtMSIndex, "200"},
		{jobCompletedAtMSIndex, "200"},
	} {
		recordAuthoritySet(record, change.index, change.value)
	}
	return record
}

func recordAuthorityReservationRecord(t *testing.T, state string) Record {
	t.Helper()
	canonicalURL := "https://example.com/path"
	target := RequestTarget{URLID: recordAuthorityURLID(canonicalURL), CanonicalURL: canonicalURL}
	rateScopeID := RateScopeID(strings.Repeat("2", 32))
	decision, err := NewPolicyDecision(PolicyDecisionInput{
		RequestKind: RequestDocument, Target: target, Depth: 2, GroupID: GroupID("default"), RateScopeID: rateScopeID,
		GroupConcurrency: 3, GroupIntervalMS: 100, OriginConcurrency: 3, OriginIntervalMS: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease := LeaseIdentity{
		RunID: RunID(strings.Repeat("1", 32)), JobID: JobID(strings.Repeat("3", 64)),
		OwnerID: OwnerID(strings.Repeat("4", 32)), Fence: Fence(1), Token: LeaseToken(strings.Repeat("5", 64)),
	}
	policyDigest := Digest(strings.Repeat("a", 64))
	intent := ReservationIntent{Lease: lease, RequestOrdinal: 1, Target: target, CrawlPolicyDigest: policyDigest, Decision: decision}
	group := PolicyGroup{
		GroupID: decision.GroupID, RateScopeID: decision.RateScopeID, GroupScopeID: decision.GroupScopeID,
		RequestStartLimit: MaxRequestStartsPerGroup, Concurrency: decision.GroupConcurrency, IntervalMS: decision.GroupIntervalMS,
	}
	runPolicy := newAuthenticatedTestRunPolicyAuthority(t, lease.RunID, policyDigest, plainSHA256(testDenyAllRenderPolicyArtifact()), []PolicyGroup{group})
	reservationID, err := DeriveReservationID(runPolicy, intent)
	if err != nil {
		t.Fatal(err)
	}
	decisionDigest, err := DerivePolicyDecisionDigest(decision)
	if err != nil {
		t.Fatal(err)
	}

	values := []string{
		"2", string(reservationID), string(lease.RunID), string(lease.JobID), string(lease.OwnerID), string(lease.Token),
		"1", "1", state, string(decision.RequestKind), string(target.URLID), target.CanonicalURL,
		string(decision.TargetDigest), string(policyDigest), string(decisionDigest), string(decision.GroupID), string(decision.RateScopeID),
		string(decision.GlobalScopeID), string(decision.GroupScopeID), string(decision.OriginScopeID),
		"2", "0", "3", "100", "3", "100", "100", "0", "0", "0", "0", "0", "0", "1000",
	}
	switch state {
	case "started":
		values[reservationStartedAtMSIndex] = "200"
		values[reservationDeliveryAttemptsAfterStartIndex] = "1"
		values[reservationJobStartsAfterStartIndex] = "1"
		values[reservationRunStartsAfterStartIndex] = "2"
		values[reservationGroupStartsAfterStartIndex] = "1"
	case "finished":
		values[reservationStartedAtMSIndex] = "200"
		values[reservationTerminalAtMSIndex] = "300"
		values[reservationDeliveryAttemptsAfterStartIndex] = "1"
		values[reservationJobStartsAfterStartIndex] = "1"
		values[reservationRunStartsAfterStartIndex] = "2"
		values[reservationGroupStartsAfterStartIndex] = "1"
	case "cancelled":
		values[reservationTerminalAtMSIndex] = "200"
	case "expired":
		values[reservationTerminalAtMSIndex] = "1000"
	}
	return recordAuthorityRecord(t, SchemaReservation, values)
}

func recordAuthorityRunRecord(t *testing.T, state string) Record {
	t.Helper()
	digest := strings.Repeat("a", 64)
	values := make([]string, runTerminalReasonIndex+1)
	for _, index := range []int{
		runExpectedSeedCountIndex,
		runAuthorizationExpiresAtMSIndex,
		runCanonicalizationVersionIndex,
		runCrawlPolicyVersionIndex,
		runRenderPolicyVersionIndex,
		runPolicyGroupCountIndex,
		runMaxJobsIndex,
		runMaxRequestStartsIndex,
		runGlobalConcurrencyLimitIndex,
		runMaxDeliveryAttemptsIndex,
		runJobCountIndex,
		runOpenJobCountIndex,
		runRequestStartsIndex,
		runReservationCreationsTotalIndex,
		runPendingRequestReservationsIndex,
		runStartedRequestReservationsIndex,
		runClaimsTotalIndex,
		runRetriesTotalIndex,
		runRecoveredLeasesTotalIndex,
		runRenewalRejectionsTotalIndex,
		runCompletedTotalIndex,
		runDeadTotalIndex,
		runCancelledTotalIndex,
		runOutputCommitsTotalIndex,
		runLoadRevisionIndex,
		runAuditRevisionIndex,
		runAuditCountIndex,
		runCreatedAtMSIndex,
		runSealedAtMSIndex,
		runActivatedAtMSIndex,
		runBudgetExhaustedAtMSIndex,
		runCancelledAtMSIndex,
		runCompletedAtMSIndex,
		runFinalizedAtMSIndex,
		runLastActivityAtMSIndex,
		runLastExecutionAtMSIndex,
		runLastRequestStartedAtMSIndex,
		runLastTerminalTransitionAtMSIndex,
		runRetentionAnchorMSIndex,
		runArchivedAtMSIndex,
		runPurgeStartedAtMSIndex,
		runPurgedJobCountIndex,
	} {
		values[index] = "0"
	}
	values[runProtocolVersionIndex] = "2"
	values[runContractSHA256Index] = digest
	values[runStateIndex] = state
	values[runSourceKindIndex] = "mongo"
	for _, index := range []int{
		runSourceSHA256Index,
		runAuthorizationSHA256Index,
		runAuthorizationScopeSHA256Index,
		runCanonicalizationSHA256Index,
		runCrawlPolicySHA256Index,
		runRenderPolicySHA256Index,
		runPolicyGroupMapSHA256Index,
	} {
		values[index] = digest
	}
	values[runExpectedSeedCountIndex] = "2"
	values[runAuthorizationExpiresAtMSIndex] = "1000"
	values[runCanonicalizationVersionIndex] = "1"
	values[runCrawlPolicyVersionIndex] = "2"
	values[runRenderPolicyVersionIndex] = "1"
	values[runPolicyGroupCountIndex] = "1"
	values[runMaxJobsIndex] = "10000"
	values[runMaxRequestStartsIndex] = "10"
	values[runGlobalConcurrencyLimitIndex] = "2"
	values[runMaxDeliveryAttemptsIndex] = "3"
	values[runJobCountIndex] = "2"
	values[runOpenJobCountIndex] = "2"
	values[runLoadRevisionIndex] = "2"
	values[runAuditCompleteIndex] = "0"
	values[runCreatedAtMSIndex] = "100"
	values[runLastActivityAtMSIndex] = "150"
	values[runPurgeStateIndex] = "none"
	values[runTerminalReasonIndex] = "none"

	setAudited := func() {
		values[runAuditRevisionIndex] = "2"
		values[runAuditCountIndex] = "2"
		values[runAuditCursorIndex] = strings.Repeat("1", 64)
		values[runAuditCompleteIndex] = "1"
		values[runSealedAtMSIndex] = "300"
		values[runLastActivityAtMSIndex] = "300"
	}
	setActive := func() {
		setAudited()
		values[runActivatedAtMSIndex] = "400"
		values[runJobCountIndex] = "3"
		values[runOpenJobCountIndex] = "1"
		values[runRequestStartsIndex] = "2"
		values[runReservationCreationsTotalIndex] = "2"
		values[runClaimsTotalIndex] = "2"
		values[runCompletedTotalIndex] = "1"
		values[runDeadTotalIndex] = "1"
		values[runOutputCommitsTotalIndex] = "1"
		values[runLastActivityAtMSIndex] = "550"
		values[runLastExecutionAtMSIndex] = "500"
		values[runLastRequestStartedAtMSIndex] = "500"
		values[runLastTerminalTransitionAtMSIndex] = "500"
	}

	switch state {
	case "loading":
	case "auditing":
		values[runAuditRevisionIndex] = "2"
		values[runAuditCountIndex] = "1"
		values[runAuditCursorIndex] = strings.Repeat("1", 64)
		values[runLastActivityAtMSIndex] = "200"
	case "sealed":
		setAudited()
	case "active":
		setActive()
	case "completed":
		setActive()
		values[runOpenJobCountIndex] = "0"
		values[runCompletedTotalIndex] = "2"
		values[runCompletedAtMSIndex] = "600"
		values[runFinalizedAtMSIndex] = "600"
		values[runLastActivityAtMSIndex] = "600"
		values[runLastTerminalTransitionAtMSIndex] = "550"
		values[runRetentionAnchorMSIndex] = "600"
		values[runTerminalReasonIndex] = "all_jobs_terminal"
	case "budget_exhausted":
		setActive()
		values[runRequestStartsIndex] = "10"
		values[runReservationCreationsTotalIndex] = "10"
		values[runBudgetExhaustedAtMSIndex] = "600"
		values[runFinalizedAtMSIndex] = "600"
		values[runLastActivityAtMSIndex] = "600"
		values[runRetentionAnchorMSIndex] = "600"
		values[runTerminalReasonIndex] = "request_budget_exhausted"
	case "cancelled":
		setActive()
		values[runOpenJobCountIndex] = "0"
		values[runCancelledTotalIndex] = "1"
		values[runCancelledAtMSIndex] = "575"
		values[runFinalizedAtMSIndex] = "600"
		values[runLastActivityAtMSIndex] = "600"
		values[runLastTerminalTransitionAtMSIndex] = "575"
		values[runRetentionAnchorMSIndex] = "600"
		values[runTerminalReasonIndex] = "operator_cancelled"
	case "archived":
		setActive()
		values[runOpenJobCountIndex] = "0"
		values[runCompletedTotalIndex] = "2"
		values[runCompletedAtMSIndex] = "600"
		values[runFinalizedAtMSIndex] = "600"
		values[runLastActivityAtMSIndex] = "600"
		values[runLastTerminalTransitionAtMSIndex] = "550"
		values[runRetentionAnchorMSIndex] = "600"
		values[runArchivedAtMSIndex] = "1000"
		values[runArchiveSHA256Index] = strings.Repeat("b", 64)
		values[runTerminalReasonIndex] = "all_jobs_terminal"
	default:
		t.Fatalf("unsupported run fixture state %q", state)
	}
	return recordAuthorityRecord(t, SchemaRun, values)
}

func recordAuthorityRateScopeRecord(t *testing.T) Record {
	t.Helper()
	rateScopeID := RateScopeID(strings.Repeat("2", 32))
	scopeID, err := DeriveGroupScopeID(rateScopeID)
	if err != nil {
		t.Fatal(err)
	}
	values := []string{
		"2", string(scopeID), "group", string(rateScopeID), "3", "100", "400", "300",
		"0", "0", "0", strings.Repeat("a", 64), strings.Repeat("b", 64), "300",
	}
	return recordAuthorityRecord(t, SchemaRateScope, values)
}

func recordAuthorityUnsealedStageRecord(t *testing.T, abandoned string) Record {
	t.Helper()
	runID := RunID(strings.Repeat("1", 32))
	jobID := JobID(strings.Repeat("2", 64))
	outputDigest := Digest(strings.Repeat("7", 64))
	publicationID, err := DerivePublicationID(PublicationIdentity{
		RunID: runID, JobID: jobID, Fence: Fence(1), OutputDigest: outputDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	values := []string{
		"2", string(runID), string(jobID), strings.Repeat("3", 32), "1",
		strings.Repeat("4", 64), strings.Repeat("5", 64), string(publicationID), string(outputDigest),
		"0", "1",
		"100", canonicalDecimal(100 + StageTTLMilliseconds), "0", "0", abandoned, "10", "65", "1", "1", "1",
		"0", "0", "0", "0", "0", "0", "0", "0", "0", "2",
		"", "", "", "", "", "", "", "", "", "", "", "",
	}
	return recordAuthorityRecord(t, SchemaStageMeta, values)
}

func recordAuthoritySealedStageRecord(t *testing.T, abandoned string) Record {
	t.Helper()
	record := recordAuthorityUnsealedStageRecord(t, abandoned)
	digest := strings.Repeat("a", 64)
	for _, change := range []struct {
		index int
		value string
	}{
		{stageSealedIndex, "1"},
		{stageSealedAtMSIndex, "200"},
		{stagePageFieldsWrittenIndex, "10"},
		{stageHTMLWrittenIndex, "1"},
		{stageOriginalHTMLWrittenIndex, "1"},
		{stageOutlinksWrittenIndex, "65"},
		{stageDiscoveriesWrittenIndex, "1"},
		{stageAliasesWrittenIndex, "1"},
		{stageImagesWrittenIndex, "1"},
		{stageManifestWrittenIndex, "1"},
		{stageDataBytesIndex, "1"},
		{stageKeyCountIndex, "10"},
		{stagePageFieldsChunkDigestIndex, digest},
		{stageHTMLChunkDigestIndex, digest},
		{stageOriginalHTMLChunkDigestIndex, digest},
		{stageOutlinksChunk0DigestIndex, digest},
		{stageOutlinksChunk1DigestIndex, digest},
		{stageDiscoveriesChunk0DigestIndex, digest},
		{stageAliasesChunk0DigestIndex, digest},
		{stageImagesChunk0DigestIndex, digest},
		{stageManifestChunkDigestIndex, digest},
	} {
		recordAuthoritySet(record, change.index, change.value)
	}
	return record
}

func recordAuthorityRecord(t *testing.T, schema RecordSchema, values []string) Record {
	t.Helper()
	names, err := RecordSchemaFields(schema)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != len(values) {
		t.Fatalf("fixture field count = %d, want %d", len(values), len(names))
	}
	record := make(Record, len(names))
	for index := range names {
		record[index] = textField(names[index], values[index])
	}
	return record
}

func recordAuthoritySet(record Record, index int, value string) {
	record[index].Value = []byte(value)
}

func recordAuthorityURLID(canonicalURL string) JobID {
	payload := append([]byte("mifolyo-url:v1\x00"), []byte(canonicalURL)...)
	digest := sha256.Sum256(payload)
	return JobID(hex.EncodeToString(digest[:]))
}
