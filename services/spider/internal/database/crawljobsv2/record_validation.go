package crawljobsv2

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrRecordRelation = errors.New("crawljobsv2: fixed-record relation mismatch")

const (
	runProtocolVersionIndex = iota
	runContractSHA256Index
	runStateIndex
	runSourceKindIndex
	runSourceSHA256Index
	runExpectedSeedCountIndex
	runAuthorizationSHA256Index
	runAuthorizationScopeSHA256Index
	runAuthorizationExpiresAtMSIndex
	runCanonicalizationVersionIndex
	runCanonicalizationSHA256Index
	runCrawlPolicyVersionIndex
	runCrawlPolicySHA256Index
	runRenderPolicyVersionIndex
	runRenderPolicySHA256Index
	runPolicyGroupCountIndex
	runPolicyGroupMapSHA256Index
	runMaxJobsIndex
	runMaxRequestStartsIndex
	runGlobalConcurrencyLimitIndex
	runMaxDeliveryAttemptsIndex
	runJobCountIndex
	runOpenJobCountIndex
	runRequestStartsIndex
	runReservationCreationsTotalIndex
	runPendingRequestReservationsIndex
	runStartedRequestReservationsIndex
	runClaimsTotalIndex
	runRetriesTotalIndex
	runRecoveredLeasesTotalIndex
	runRenewalRejectionsTotalIndex
	runCompletedTotalIndex
	runDeadTotalIndex
	runCancelledTotalIndex
	runOutputCommitsTotalIndex
	runLoadRevisionIndex
	runAuditRevisionIndex
	runAuditCountIndex
	runAuditCursorIndex
	runAuditCompleteIndex
	runCreatedAtMSIndex
	runSealedAtMSIndex
	runActivatedAtMSIndex
	runBudgetExhaustedAtMSIndex
	runCancelledAtMSIndex
	runCompletedAtMSIndex
	runFinalizedAtMSIndex
	runLastActivityAtMSIndex
	runLastExecutionAtMSIndex
	runLastRequestStartedAtMSIndex
	runLastTerminalTransitionAtMSIndex
	runRetentionAnchorMSIndex
	runArchivedAtMSIndex
	runArchiveSHA256Index
	runPurgeStateIndex
	runPurgeEvidenceSHA256Index
	runPurgeStartedAtMSIndex
	runPurgedJobCountIndex
	runTerminalReasonIndex
)

const (
	jobProtocolVersionIndex = iota
	jobRunIDIndex
	jobJobIDIndex
	jobURLIDIndex
	jobCanonicalURLIndex
	jobDepthIndex
	jobScoreTextIndex
	jobStateIndex
	jobGroupIDIndex
	jobRateScopeIDIndex
	jobGroupScopeIDIndex
	jobInitialOriginScopeIDIndex
	jobPolicyDecisionSHA256Index
	jobClaimCountIndex
	jobDeliveryAttemptsIndex
	jobRequestStartsIndex
	jobLeaseRequestStartsBaselineIndex
	jobRetryCountIndex
	jobPreIORecoveriesIndex
	jobNextRequestOrdinalIndex
	jobLastRequestStartedAtMSIndex
	jobLastDocumentRequestStartedAtMSIndex
	jobLastDocumentRequestFenceIndex
	jobLastDocumentTargetURLIDIndex
	jobLastDocumentTargetURLIndex
	jobLastDocumentTargetDigestIndex
	jobLastReasonIndex
	jobLastFailureReasonIndex
	jobLeaseOwnerIndex
	jobLeaseTokenIndex
	jobLeaseFenceIndex
	jobLeaseStartedAtMSIndex
	jobLeaseExpiresAtMSIndex
	jobLeaseDeliveryStartedIndex
	jobActiveReservationIDIndex
	jobActiveStageCommitIDIndex
	jobLastStageCommitIDIndex
	jobLastStageFenceIndex
	jobNotBeforeMSIndex
	jobCommitBackpressureFenceIndex
	jobCommitBackpressureReasonIndex
	jobCommitBackpressureStartedAtMSIndex
	jobCommitBackpressureDeadlineMSIndex
	jobOutputDigestIndex
	jobPublicationIDIndex
	jobCommitIDIndex
	jobPublishedPageKeyIndex
	jobLastTransitionIDIndex
	jobLastTransitionStatusIndex
	jobCreatedAtMSIndex
	jobUpdatedAtMSIndex
	jobCompletedAtMSIndex
	jobDeadAtMSIndex
	jobCancelledAtMSIndex
)

const (
	reservationProtocolVersionIndex = iota
	reservationIDIndex
	reservationRunIDIndex
	reservationJobIDIndex
	reservationOwnerIDIndex
	reservationLeaseTokenIndex
	reservationLeaseFenceIndex
	reservationRequestOrdinalIndex
	reservationStateIndex
	reservationRequestKindIndex
	reservationTargetURLIDIndex
	reservationCanonicalTargetURLIndex
	reservationTargetDigestIndex
	reservationCrawlPolicySHA256Index
	reservationPolicyDecisionSHA256Index
	reservationGroupIDIndex
	reservationRateScopeIDIndex
	reservationGlobalScopeIDIndex
	reservationGroupScopeIDIndex
	reservationOriginScopeIDIndex
	reservationGlobalConcurrencyIndex
	reservationGlobalIntervalMSIndex
	reservationGroupConcurrencyIndex
	reservationGroupIntervalMSIndex
	reservationOriginConcurrencyIndex
	reservationOriginIntervalMSIndex
	reservationCreatedAtMSIndex
	reservationStartedAtMSIndex
	reservationTerminalAtMSIndex
	reservationDeliveryAttemptsAfterStartIndex
	reservationJobStartsAfterStartIndex
	reservationRunStartsAfterStartIndex
	reservationGroupStartsAfterStartIndex
	reservationExpiresAtMSIndex
)

const (
	rateScopeProtocolVersionIndex = iota
	rateScopeIDIndex
	rateScopeKindIndex
	rateScopeWitnessIndex
	rateScopeEffectiveConcurrencyIndex
	rateScopeEffectiveIntervalMSIndex
	rateScopeNextAllowedMSIndex
	rateScopeLastStartedAtMSIndex
	rateScopeActiveCountIndex
	rateScopePendingCountIndex
	rateScopeStartedCountIndex
	rateScopeConcurrencySourceSHA256Index
	rateScopeIntervalSourceSHA256Index
	rateScopeUpdatedAtMSIndex
)

const (
	stageProtocolVersionIndex = iota
	stageRunIDIndex
	stageJobIDIndex
	stageOwnerIDIndex
	stageLeaseFenceIndex
	stageTokenDigestIndex
	stageCommitIDIndex
	stagePublicationIDIndex
	stageOutputDigestIndex
	stageRequestStartsBaselineIndex
	stageRequestStartsGenerationIndex
	stageCreatedAtMSIndex
	stageExpiresAtMSIndex
	stageSealedIndex
	stageSealedAtMSIndex
	stageAbandonedIndex
	stageExpectedPageFieldsIndex
	stageExpectedOutlinksIndex
	stageExpectedDiscoveriesIndex
	stageExpectedAliasesIndex
	stageExpectedImagesIndex
	stagePageFieldsWrittenIndex
	stageHTMLWrittenIndex
	stageOriginalHTMLWrittenIndex
	stageOutlinksWrittenIndex
	stageDiscoveriesWrittenIndex
	stageAliasesWrittenIndex
	stageImagesWrittenIndex
	stageManifestWrittenIndex
	stageDataBytesIndex
	stageKeyCountIndex
	stagePageFieldsChunkDigestIndex
	stageHTMLChunkDigestIndex
	stageOriginalHTMLChunkDigestIndex
	stageOutlinksChunk0DigestIndex
	stageOutlinksChunk1DigestIndex
	stageOutlinksChunk2DigestIndex
	stageOutlinksChunk3DigestIndex
	stageDiscoveriesChunk0DigestIndex
	stageDiscoveriesChunk1DigestIndex
	stageAliasesChunk0DigestIndex
	stageImagesChunk0DigestIndex
	stageManifestChunkDigestIndex
)

// ValidateRecord is the value-aware fixed-record entry point. In contrast to
// ValidateRecordSchema, it validates ordered names, lexical values, protocol
// constants, sentinels, and the bounded cross-field relations available from a
// record in isolation.
func ValidateRecord(schema RecordSchema, record Record) error {
	expected, err := RecordSchemaFields(schema)
	if err != nil {
		return err
	}
	if len(record) != len(expected) {
		return ErrInvalidSchema
	}
	for index := range expected {
		if record[index].Name != expected[index] {
			return ErrInvalidSchema
		}
	}
	switch schema {
	case SchemaRun, SchemaJob, SchemaReservation, SchemaRateScope, SchemaStageMeta:
		return validateLedgerRecord(schema, record)
	default:
		return validateAuthorityRecord(schema, record)
	}
}

func validateLedgerRecord(schema RecordSchema, record Record) error {
	values, err := strictTextValues(record)
	if err != nil {
		return err
	}
	if values[0] != "2" {
		return ErrInvalidRecordValue
	}
	switch schema {
	case SchemaRun:
		return validateRunRecordValues(values)
	case SchemaJob:
		return validateJobRecordValues(values)
	case SchemaReservation:
		return validateReservationRecordValues(values)
	case SchemaRateScope:
		return validateRateScopeRecordValues(values)
	case SchemaStageMeta:
		return validateStageMetaRecordValues(values)
	default:
		return ErrInvalidSchema
	}
}

func validateRunRecordValues(values []string) error {
	for _, index := range []int{
		runContractSHA256Index,
		runSourceSHA256Index,
		runAuthorizationSHA256Index,
		runAuthorizationScopeSHA256Index,
		runCanonicalizationSHA256Index,
		runCrawlPolicySHA256Index,
		runRenderPolicySHA256Index,
		runPolicyGroupMapSHA256Index,
	} {
		if _, err := parseNonzeroDigest(values[index]); err != nil {
			return err
		}
	}
	state := values[runStateIndex]
	if !oneOf(state, "loading", "auditing", "sealed", "active", "completed", "budget_exhausted", "cancelled", "archived") {
		return ErrInvalidRecordValue
	}
	sourceKind, err := ParseSourceKind(values[runSourceKindIndex])
	if err != nil {
		return err
	}
	numericIndexes := []int{
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
	}
	numbers, err := parseIndexedDecimals(values, numericIndexes)
	if err != nil {
		return err
	}

	expectedSeedCount := numbers[runExpectedSeedCountIndex]
	authorizationExpiresAtMS := numbers[runAuthorizationExpiresAtMSIndex]
	policyGroupCount := numbers[runPolicyGroupCountIndex]
	maxJobs := numbers[runMaxJobsIndex]
	maxRequestStarts := numbers[runMaxRequestStartsIndex]
	jobCount := numbers[runJobCountIndex]
	openJobCount := numbers[runOpenJobCountIndex]
	requestStarts := numbers[runRequestStartsIndex]
	reservationCreations := numbers[runReservationCreationsTotalIndex]
	pendingReservations := numbers[runPendingRequestReservationsIndex]
	startedReservations := numbers[runStartedRequestReservationsIndex]
	claimsTotal := numbers[runClaimsTotalIndex]
	retriesTotal := numbers[runRetriesTotalIndex]
	recoveredLeasesTotal := numbers[runRecoveredLeasesTotalIndex]
	completedTotal := numbers[runCompletedTotalIndex]
	deadTotal := numbers[runDeadTotalIndex]
	cancelledTotal := numbers[runCancelledTotalIndex]
	terminalTotal := completedTotal + deadTotal + cancelledTotal
	createdAtMS := numbers[runCreatedAtMSIndex]
	lastActivityAtMS := numbers[runLastActivityAtMSIndex]
	lastExecutionAtMS := numbers[runLastExecutionAtMSIndex]
	lastRequestStartedAtMS := numbers[runLastRequestStartedAtMSIndex]
	lastTerminalTransitionAtMS := numbers[runLastTerminalTransitionAtMSIndex]

	if expectedSeedCount > MaxJobsPerRun || (sourceKind == SourceV1Migration && expectedSeedCount == 0) ||
		policyGroupCount == 0 || policyGroupCount > MaxPolicyGroupsPerRun || maxJobs != MaxJobsPerRun ||
		maxRequestStarts == 0 || maxRequestStarts > MaxRequestStartsPerRun ||
		numbers[runGlobalConcurrencyLimitIndex] != GlobalActiveRequestLimit || numbers[runMaxDeliveryAttemptsIndex] != MaxDeliveryAttempts ||
		jobCount > maxJobs || openJobCount > jobCount || requestStarts > maxRequestStarts || reservationCreations > MaxReservationCreationsPerRun ||
		requestStarts > reservationCreations || pendingReservations+startedReservations > reservationCreations || claimsTotal > reservationCreations ||
		retriesTotal > claimsTotal || retriesTotal > requestStarts || recoveredLeasesTotal > claimsTotal ||
		terminalTotal > jobCount || openJobCount != jobCount-terminalTotal || numbers[runOutputCommitsTotalIndex] > completedTotal ||
		numbers[runLoadRevisionIndex] == 0 || numbers[runAuditRevisionIndex] > numbers[runLoadRevisionIndex] ||
		numbers[runAuditCountIndex] > jobCount || numbers[runPurgedJobCountIndex] > jobCount || createdAtMS == 0 || lastActivityAtMS < createdAtMS {
		return ErrRecordRelation
	}
	if authorizationExpiresAtMS <= createdAtMS || authorizationExpiresAtMS-createdAtMS > MaxAuthorizationWindowMilliseconds {
		return ErrRecordRelation
	}
	if values[runCanonicalizationVersionIndex] != "1" || values[runCrawlPolicyVersionIndex] != "2" ||
		numbers[runRenderPolicyVersionIndex] == 0 || values[runMaxDeliveryAttemptsIndex] != "3" ||
		!oneOf(values[runAuditCompleteIndex], "0", "1") {
		return ErrInvalidRecordValue
	}
	if (values[runAuditCursorIndex] == "") != (numbers[runAuditCountIndex] == 0) {
		return ErrRecordRelation
	}
	if values[runAuditCursorIndex] != "" {
		if _, err := ParseJobID(values[runAuditCursorIndex]); err != nil {
			return err
		}
	}
	if (requestStarts == 0) != (lastRequestStartedAtMS == 0) ||
		(terminalTotal == 0) != (lastTerminalTransitionAtMS == 0) ||
		lastExecutionAtMS > lastActivityAtMS || lastRequestStartedAtMS > lastActivityAtMS || lastTerminalTransitionAtMS > lastActivityAtMS {
		return ErrRecordRelation
	}
	for _, index := range []int{
		runSealedAtMSIndex,
		runActivatedAtMSIndex,
		runBudgetExhaustedAtMSIndex,
		runCancelledAtMSIndex,
		runCompletedAtMSIndex,
		runFinalizedAtMSIndex,
		runLastExecutionAtMSIndex,
		runLastRequestStartedAtMSIndex,
		runLastTerminalTransitionAtMSIndex,
		runRetentionAnchorMSIndex,
		runArchivedAtMSIndex,
		runPurgeStartedAtMSIndex,
	} {
		if numbers[index] != 0 && numbers[index] < createdAtMS {
			return ErrRecordRelation
		}
	}
	if numbers[runSealedAtMSIndex] != 0 && numbers[runSealedAtMSIndex] > lastActivityAtMS ||
		numbers[runActivatedAtMSIndex] != 0 && numbers[runActivatedAtMSIndex] > lastActivityAtMS ||
		numbers[runBudgetExhaustedAtMSIndex] != 0 && numbers[runBudgetExhaustedAtMSIndex] > lastActivityAtMS ||
		numbers[runCancelledAtMSIndex] != 0 && numbers[runCancelledAtMSIndex] > lastActivityAtMS ||
		numbers[runCompletedAtMSIndex] != 0 && numbers[runCompletedAtMSIndex] > lastActivityAtMS ||
		numbers[runFinalizedAtMSIndex] != 0 && numbers[runFinalizedAtMSIndex] > lastActivityAtMS {
		return ErrRecordRelation
	}
	if numbers[runActivatedAtMSIndex] != 0 &&
		(numbers[runSealedAtMSIndex] == 0 || numbers[runActivatedAtMSIndex] < numbers[runSealedAtMSIndex]) {
		return ErrRecordRelation
	}
	if numbers[runBudgetExhaustedAtMSIndex] != 0 &&
		(numbers[runActivatedAtMSIndex] == 0 || numbers[runBudgetExhaustedAtMSIndex] < numbers[runActivatedAtMSIndex]) {
		return ErrRecordRelation
	}
	if numbers[runCompletedAtMSIndex] != 0 &&
		(numbers[runActivatedAtMSIndex] == 0 || numbers[runCompletedAtMSIndex] < numbers[runActivatedAtMSIndex]) {
		return ErrRecordRelation
	}

	archiveSHA256 := values[runArchiveSHA256Index]
	if (archiveSHA256 == "") != (numbers[runArchivedAtMSIndex] == 0) {
		return ErrRecordRelation
	}
	if archiveSHA256 != "" {
		if _, err := parseNonzeroDigest(archiveSHA256); err != nil {
			return err
		}
	}
	switch values[runPurgeStateIndex] {
	case "none":
		if values[runPurgeEvidenceSHA256Index] != "" || numbers[runPurgeStartedAtMSIndex] != 0 || numbers[runPurgedJobCountIndex] != 0 {
			return ErrRecordRelation
		}
	case "in_progress":
		if _, err := parseNonzeroDigest(values[runPurgeEvidenceSHA256Index]); err != nil {
			return err
		}
		if numbers[runPurgeStartedAtMSIndex] == 0 || numbers[runPurgedJobCountIndex] == 0 || numbers[runPurgedJobCountIndex] >= jobCount {
			return ErrRecordRelation
		}
	default:
		return ErrInvalidRecordValue
	}
	if _, err := ParseReason(values[runTerminalReasonIndex]); err != nil {
		return err
	}

	if numbers[runSealedAtMSIndex] != 0 && numbers[runSealedAtMSIndex] < createdAtMS ||
		numbers[runCancelledAtMSIndex] != 0 && numbers[runCancelledAtMSIndex] < runLifecyclePrefixAtMS(numbers) ||
		numbers[runFinalizedAtMSIndex] != 0 && numbers[runFinalizedAtMSIndex] < runTerminalStateAtMS(numbers) ||
		numbers[runArchivedAtMSIndex] != 0 && numbers[runArchivedAtMSIndex] < numbers[runFinalizedAtMSIndex] {
		return ErrRecordRelation
	}

	switch state {
	case "loading":
		if err := validateRunAuditNotStarted(values, numbers); err != nil || jobCount > expectedSeedCount ||
			!runPreExecutionCountersAreZero(numbers) || terminalTotal != 0 ||
			!runNumbersAreZero(numbers, runSealedAtMSIndex, runActivatedAtMSIndex, runBudgetExhaustedAtMSIndex, runCancelledAtMSIndex,
				runCompletedAtMSIndex, runFinalizedAtMSIndex, runRetentionAnchorMSIndex, runArchivedAtMSIndex) ||
			values[runTerminalReasonIndex] != "none" || !runPurgeIsClear(values, numbers) {
			return ErrRecordRelation
		}
	case "auditing":
		if err := validateRunAuditInProgress(values, numbers); err != nil || jobCount > expectedSeedCount ||
			!runPreExecutionCountersAreZero(numbers) || terminalTotal != 0 ||
			!runNumbersAreZero(numbers, runSealedAtMSIndex, runActivatedAtMSIndex, runBudgetExhaustedAtMSIndex, runCancelledAtMSIndex,
				runCompletedAtMSIndex, runFinalizedAtMSIndex, runRetentionAnchorMSIndex, runArchivedAtMSIndex) ||
			values[runTerminalReasonIndex] != "none" || !runPurgeIsClear(values, numbers) {
			return ErrRecordRelation
		}
	case "sealed":
		if err := validateRunAuditComplete(values, numbers, false); err != nil ||
			!runPreExecutionCountersAreZero(numbers) || terminalTotal != 0 || numbers[runSealedAtMSIndex] == 0 ||
			!runNumbersAreZero(numbers, runActivatedAtMSIndex, runBudgetExhaustedAtMSIndex, runCancelledAtMSIndex,
				runCompletedAtMSIndex, runFinalizedAtMSIndex, runRetentionAnchorMSIndex, runArchivedAtMSIndex) ||
			values[runTerminalReasonIndex] != "none" || !runPurgeIsClear(values, numbers) {
			return ErrRecordRelation
		}
	case "active":
		if err := validateRunAuditComplete(values, numbers, true); err != nil ||
			numbers[runSealedAtMSIndex] == 0 || numbers[runActivatedAtMSIndex] == 0 ||
			!runNumbersAreZero(numbers, runBudgetExhaustedAtMSIndex, runCancelledAtMSIndex, runCompletedAtMSIndex,
				runFinalizedAtMSIndex, runRetentionAnchorMSIndex, runArchivedAtMSIndex) ||
			values[runTerminalReasonIndex] != "none" || !runPurgeIsClear(values, numbers) {
			return ErrRecordRelation
		}
	case "completed":
		if err := validateRunAuditComplete(values, numbers, true); err != nil ||
			numbers[runSealedAtMSIndex] == 0 || numbers[runActivatedAtMSIndex] == 0 || openJobCount != 0 ||
			pendingReservations != 0 || startedReservations != 0 || numbers[runCompletedAtMSIndex] == 0 ||
			numbers[runCompletedAtMSIndex] != numbers[runFinalizedAtMSIndex] ||
			!runNumbersAreZero(numbers, runBudgetExhaustedAtMSIndex, runCancelledAtMSIndex, runArchivedAtMSIndex) ||
			values[runTerminalReasonIndex] != "all_jobs_terminal" || validateRunFinalization(numbers) != nil || !runPurgeIsClear(values, numbers) {
			return ErrRecordRelation
		}
	case "budget_exhausted":
		// Budget exhaustion is finalized with ready/delayed jobs retained as
		// evidence, unlike completed and fully finalized cancellation.
		if err := validateRunAuditComplete(values, numbers, true); err != nil ||
			numbers[runSealedAtMSIndex] == 0 || numbers[runActivatedAtMSIndex] == 0 || openJobCount == 0 ||
			pendingReservations != 0 || startedReservations != 0 || numbers[runBudgetExhaustedAtMSIndex] == 0 ||
			numbers[runBudgetExhaustedAtMSIndex] != numbers[runFinalizedAtMSIndex] ||
			!runNumbersAreZero(numbers, runCancelledAtMSIndex, runCompletedAtMSIndex, runArchivedAtMSIndex) ||
			validateRunBudgetReason(values[runTerminalReasonIndex], numbers) != nil || validateRunFinalization(numbers) != nil ||
			!runPurgeIsClear(values, numbers) {
			return ErrRecordRelation
		}
	case "cancelled":
		// Cancellation is observable while bounded maintenance is still draining
		// open jobs; zero open work becomes mandatory once finalization is recorded.
		if err := validateRunCancelledAuditPrefix(values, numbers); err != nil || numbers[runCancelledAtMSIndex] == 0 ||
			!runNumbersAreZero(numbers, runBudgetExhaustedAtMSIndex, runCompletedAtMSIndex, runArchivedAtMSIndex) ||
			!oneOf(values[runTerminalReasonIndex], "authorization_expired", "operator_cancelled", "source_cancelled") {
			return ErrRecordRelation
		}
		if numbers[runActivatedAtMSIndex] == 0 && (!runPreExecutionCountersAreZero(numbers) || completedTotal != 0 || deadTotal != 0) {
			return ErrRecordRelation
		}
		if numbers[runFinalizedAtMSIndex] == 0 {
			if numbers[runRetentionAnchorMSIndex] != 0 {
				return ErrRecordRelation
			}
		} else if openJobCount != 0 || pendingReservations != 0 || startedReservations != 0 || validateRunFinalization(numbers) != nil {
			return ErrRecordRelation
		}
		if values[runPurgeStateIndex] == "in_progress" &&
			(sourceKind != SourceV1Migration || numbers[runActivatedAtMSIndex] != 0 || openJobCount != 0 ||
				requestStarts != 0 || reservationCreations != 0 || claimsTotal != 0 || pendingReservations != 0 || startedReservations != 0) {
			return ErrRecordRelation
		}
	case "archived":
		if numbers[runArchivedAtMSIndex] == 0 || archiveSHA256 == "" || numbers[runFinalizedAtMSIndex] == 0 ||
			pendingReservations != 0 || startedReservations != 0 || validateRunFinalization(numbers) != nil ||
			values[runPurgeStateIndex] == "in_progress" && values[runPurgeEvidenceSHA256Index] != archiveSHA256 {
			return ErrRecordRelation
		}
		switch values[runTerminalReasonIndex] {
		case "all_jobs_terminal":
			if err := validateRunAuditComplete(values, numbers, true); err != nil || openJobCount != 0 ||
				numbers[runSealedAtMSIndex] == 0 || numbers[runActivatedAtMSIndex] == 0 ||
				numbers[runCompletedAtMSIndex] == 0 || numbers[runCompletedAtMSIndex] != numbers[runFinalizedAtMSIndex] ||
				!runNumbersAreZero(numbers, runBudgetExhaustedAtMSIndex, runCancelledAtMSIndex) {
				return ErrRecordRelation
			}
		case "request_budget_exhausted", "reservation_limit_exhausted", "group_budgets_exhausted":
			if err := validateRunAuditComplete(values, numbers, true); err != nil || openJobCount == 0 ||
				numbers[runSealedAtMSIndex] == 0 || numbers[runActivatedAtMSIndex] == 0 ||
				numbers[runBudgetExhaustedAtMSIndex] == 0 || numbers[runBudgetExhaustedAtMSIndex] != numbers[runFinalizedAtMSIndex] ||
				!runNumbersAreZero(numbers, runCancelledAtMSIndex, runCompletedAtMSIndex) ||
				validateRunBudgetReason(values[runTerminalReasonIndex], numbers) != nil {
				return ErrRecordRelation
			}
		case "authorization_expired", "operator_cancelled", "source_cancelled":
			if err := validateRunCancelledAuditPrefix(values, numbers); err != nil || openJobCount != 0 ||
				numbers[runCancelledAtMSIndex] == 0 ||
				!runNumbersAreZero(numbers, runBudgetExhaustedAtMSIndex, runCompletedAtMSIndex) {
				return ErrRecordRelation
			}
			if numbers[runActivatedAtMSIndex] == 0 && !runPreExecutionCountersAreZero(numbers) {
				return ErrRecordRelation
			}
		default:
			return ErrRecordRelation
		}
	}
	return nil
}

func validateRunAuditNotStarted(values []string, numbers map[int]uint64) error {
	if numbers[runAuditRevisionIndex] != 0 || numbers[runAuditCountIndex] != 0 ||
		values[runAuditCursorIndex] != "" || values[runAuditCompleteIndex] != "0" {
		return ErrRecordRelation
	}
	return nil
}

func validateRunAuditInProgress(values []string, numbers map[int]uint64) error {
	jobCount := numbers[runJobCountIndex]
	expectedSeedCount := numbers[runExpectedSeedCountIndex]
	auditCount := numbers[runAuditCountIndex]
	if numbers[runAuditRevisionIndex] == 0 || numbers[runAuditRevisionIndex] != numbers[runLoadRevisionIndex] ||
		jobCount > expectedSeedCount || auditCount > jobCount {
		return ErrRecordRelation
	}
	if values[runAuditCompleteIndex] == "1" {
		if auditCount != jobCount || jobCount != expectedSeedCount {
			return ErrRecordRelation
		}
	} else if jobCount != 0 && auditCount == jobCount {
		return ErrRecordRelation
	}
	return nil
}

func validateRunAuditComplete(values []string, numbers map[int]uint64, discoveriesAllowed bool) error {
	expectedSeedCount := numbers[runExpectedSeedCountIndex]
	jobCount := numbers[runJobCountIndex]
	if numbers[runAuditRevisionIndex] == 0 || numbers[runAuditRevisionIndex] != numbers[runLoadRevisionIndex] ||
		numbers[runAuditCountIndex] != expectedSeedCount || values[runAuditCompleteIndex] != "1" {
		return ErrRecordRelation
	}
	if discoveriesAllowed {
		if jobCount < expectedSeedCount {
			return ErrRecordRelation
		}
	} else if jobCount != expectedSeedCount {
		return ErrRecordRelation
	}
	return nil
}

func validateRunCancelledAuditPrefix(values []string, numbers map[int]uint64) error {
	if numbers[runActivatedAtMSIndex] != 0 {
		return validateRunAuditComplete(values, numbers, true)
	}
	if numbers[runSealedAtMSIndex] != 0 {
		return validateRunAuditComplete(values, numbers, false)
	}
	if numbers[runAuditRevisionIndex] != 0 {
		return validateRunAuditInProgress(values, numbers)
	}
	if numbers[runJobCountIndex] > numbers[runExpectedSeedCountIndex] {
		return ErrRecordRelation
	}
	return validateRunAuditNotStarted(values, numbers)
}

func validateRunBudgetReason(reason string, numbers map[int]uint64) error {
	switch reason {
	case "request_budget_exhausted":
		if numbers[runRequestStartsIndex] != numbers[runMaxRequestStartsIndex] {
			return ErrRecordRelation
		}
	case "reservation_limit_exhausted":
		if numbers[runRequestStartsIndex] >= numbers[runMaxRequestStartsIndex] ||
			numbers[runReservationCreationsTotalIndex] != MaxReservationCreationsPerRun {
			return ErrRecordRelation
		}
	case "group_budgets_exhausted":
		if numbers[runRequestStartsIndex] >= numbers[runMaxRequestStartsIndex] ||
			numbers[runReservationCreationsTotalIndex] >= MaxReservationCreationsPerRun {
			return ErrRecordRelation
		}
	default:
		return ErrRecordRelation
	}
	return nil
}

func validateRunFinalization(numbers map[int]uint64) error {
	finalizedAtMS := numbers[runFinalizedAtMSIndex]
	if finalizedAtMS == 0 {
		return ErrRecordRelation
	}
	expectedRetentionAnchor := finalizedAtMS
	for _, index := range []int{runLastRequestStartedAtMSIndex, runLastTerminalTransitionAtMSIndex, runLastActivityAtMSIndex} {
		if numbers[index] > expectedRetentionAnchor {
			expectedRetentionAnchor = numbers[index]
		}
	}
	if numbers[runRetentionAnchorMSIndex] != expectedRetentionAnchor {
		return ErrRecordRelation
	}
	return nil
}

func runLifecyclePrefixAtMS(numbers map[int]uint64) uint64 {
	for _, index := range []int{runActivatedAtMSIndex, runSealedAtMSIndex, runCreatedAtMSIndex} {
		if numbers[index] != 0 {
			return numbers[index]
		}
	}
	return 0
}

func runTerminalStateAtMS(numbers map[int]uint64) uint64 {
	terminalAtMS := uint64(0)
	for _, index := range []int{runBudgetExhaustedAtMSIndex, runCancelledAtMSIndex, runCompletedAtMSIndex} {
		if numbers[index] > terminalAtMS {
			terminalAtMS = numbers[index]
		}
	}
	return terminalAtMS
}

func runPreExecutionCountersAreZero(numbers map[int]uint64) bool {
	return runNumbersAreZero(numbers,
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
		runOutputCommitsTotalIndex,
		runLastExecutionAtMSIndex,
		runLastRequestStartedAtMSIndex,
	)
}

func runPurgeIsClear(values []string, numbers map[int]uint64) bool {
	return values[runPurgeStateIndex] == "none" && values[runPurgeEvidenceSHA256Index] == "" &&
		numbers[runPurgeStartedAtMSIndex] == 0 && numbers[runPurgedJobCountIndex] == 0
}

func runNumbersAreZero(numbers map[int]uint64, indexes ...int) bool {
	for _, index := range indexes {
		if numbers[index] != 0 {
			return false
		}
	}
	return true
}

func validateJobRecordValues(values []string) error {
	runID, err := ParseRunID(values[jobRunIDIndex])
	if err != nil {
		return err
	}
	jobID, err := ParseJobID(values[jobJobIDIndex])
	if err != nil {
		return err
	}
	if values[jobURLIDIndex] != values[jobJobIDIndex] || validateJobURL(jobID, values[jobCanonicalURLIndex]) != nil {
		return ErrRecordRelation
	}
	if _, err := ParseUnsignedDecimal(values[jobDepthIndex]); err != nil {
		return err
	}
	if _, err := ParseScoreText(values[jobScoreTextIndex]); err != nil {
		return err
	}
	state := values[jobStateIndex]
	if !oneOf(state, "ready", "leased", "delayed", "completed", "dead", "cancelled") {
		return ErrInvalidRecordValue
	}
	if _, err := ParseGroupID(values[jobGroupIDIndex]); err != nil {
		return err
	}
	rateScopeID, err := ParseRateScopeID(values[jobRateScopeIDIndex])
	if err != nil {
		return err
	}
	groupScopeID, err := parseNonzeroDigest(values[jobGroupScopeIDIndex])
	if err != nil {
		return err
	}
	initialOriginScopeID, err := parseNonzeroDigest(values[jobInitialOriginScopeIDIndex])
	if err != nil {
		return err
	}
	if _, err := parseNonzeroDigest(values[jobPolicyDecisionSHA256Index]); err != nil {
		return err
	}
	expectedGroupScopeID, err := DeriveGroupScopeID(rateScopeID)
	if err != nil || groupScopeID != expectedGroupScopeID {
		return ErrRecordRelation
	}
	origin, err := DeriveCanonicalOrigin(values[jobCanonicalURLIndex])
	if err != nil {
		return err
	}
	expectedOriginScopeID, err := DeriveOriginScopeID(origin)
	if err != nil || initialOriginScopeID != expectedOriginScopeID {
		return ErrRecordRelation
	}

	numericIndexes := []int{
		jobClaimCountIndex, jobDeliveryAttemptsIndex, jobRequestStartsIndex, jobLeaseRequestStartsBaselineIndex, jobRetryCountIndex,
		jobPreIORecoveriesIndex, jobNextRequestOrdinalIndex, jobLastRequestStartedAtMSIndex,
		jobLastDocumentRequestStartedAtMSIndex, jobLastDocumentRequestFenceIndex, jobLeaseFenceIndex,
		jobLeaseStartedAtMSIndex, jobLeaseExpiresAtMSIndex, jobLeaseDeliveryStartedIndex,
		jobLastStageFenceIndex, jobNotBeforeMSIndex, jobCommitBackpressureFenceIndex,
		jobCommitBackpressureStartedAtMSIndex, jobCommitBackpressureDeadlineMSIndex,
		jobCreatedAtMSIndex, jobUpdatedAtMSIndex, jobCompletedAtMSIndex, jobDeadAtMSIndex,
		jobCancelledAtMSIndex,
	}
	numbers, err := parseIndexedDecimals(values, numericIndexes)
	if err != nil {
		return err
	}
	claimCount := numbers[jobClaimCountIndex]
	deliveryAttempts := numbers[jobDeliveryAttemptsIndex]
	requestStarts := numbers[jobRequestStartsIndex]
	baseline := numbers[jobLeaseRequestStartsBaselineIndex]
	retryCount := numbers[jobRetryCountIndex]
	preIORecoveries := numbers[jobPreIORecoveriesIndex]
	nextRequestOrdinal := numbers[jobNextRequestOrdinalIndex]
	lastRequestStartedAtMS := numbers[jobLastRequestStartedAtMSIndex]
	lastDocumentRequestStartedAtMS := numbers[jobLastDocumentRequestStartedAtMSIndex]
	lastDocumentRequestFence := numbers[jobLastDocumentRequestFenceIndex]
	leaseFence := numbers[jobLeaseFenceIndex]
	leaseStartedAtMS := numbers[jobLeaseStartedAtMSIndex]
	leaseExpiresAtMS := numbers[jobLeaseExpiresAtMSIndex]
	leaseDeliveryStarted := numbers[jobLeaseDeliveryStartedIndex]
	lastStageFence := numbers[jobLastStageFenceIndex]
	notBeforeMS := numbers[jobNotBeforeMSIndex]
	backpressureFence := numbers[jobCommitBackpressureFenceIndex]
	backpressureStartedAtMS := numbers[jobCommitBackpressureStartedAtMSIndex]
	backpressureDeadlineMS := numbers[jobCommitBackpressureDeadlineMSIndex]
	createdAtMS := numbers[jobCreatedAtMSIndex]
	updatedAtMS := numbers[jobUpdatedAtMSIndex]
	completedAtMS := numbers[jobCompletedAtMSIndex]
	deadAtMS := numbers[jobDeadAtMSIndex]
	cancelledAtMS := numbers[jobCancelledAtMSIndex]

	if leaseFence != claimCount || deliveryAttempts > MaxDeliveryAttempts || leaseDeliveryStarted > 1 || deliveryAttempts > claimCount ||
		requestStarts < deliveryAttempts || requestStarts > MaxRequestStartsPerRun || retryCount > deliveryAttempts || retryCount >= MaxDeliveryAttempts ||
		preIORecoveries > MaxPreIOExpiredLeaseRecoveries || preIORecoveries+deliveryAttempts > claimCount ||
		nextRequestOrdinal == 0 || nextRequestOrdinal > MaxReservationCreationsPerRun+1 || nextRequestOrdinal <= claimCount || nextRequestOrdinal <= requestStarts ||
		lastStageFence > leaseFence || backpressureFence > leaseFence || createdAtMS == 0 || updatedAtMS < createdAtMS {
		return ErrRecordRelation
	}
	if baseline > requestStarts || baseline >= MaxRequestStartsPerRun ||
		claimCount == 0 && (baseline != 0 || requestStarts != 0) || !validDeliveryAttemptHistory(baseline, requestStarts, deliveryAttempts) {
		return ErrRecordRelation
	}
	priorDeliveryAttempts := deliveryAttempts
	if requestStarts > baseline {
		priorDeliveryAttempts--
	}
	// Earlier deliveries need earlier fences and must leave capacity for this claim.
	if leaseFence > 0 && (priorDeliveryAttempts >= leaseFence || priorDeliveryAttempts >= MaxDeliveryAttempts) {
		return ErrRecordRelation
	}
	if requestStarts == 0 != (lastRequestStartedAtMS == 0) || lastRequestStartedAtMS > updatedAtMS {
		return ErrRecordRelation
	}
	if err := validateOptionalDocumentIdentity(
		values[jobLastDocumentRequestStartedAtMSIndex],
		values[jobLastDocumentRequestFenceIndex],
		values[jobLastDocumentTargetURLIDIndex],
		values[jobLastDocumentTargetURLIndex],
		values[jobLastDocumentTargetDigestIndex],
	); err != nil {
		return err
	}
	if lastDocumentRequestStartedAtMS > lastRequestStartedAtMS || lastDocumentRequestFence > leaseFence {
		return ErrRecordRelation
	}
	lastReason, err := ParseReason(values[jobLastReasonIndex])
	if err != nil {
		return err
	}
	lastFailureReason, err := ParseReason(values[jobLastFailureReasonIndex])
	if err != nil {
		return err
	}
	if err := validateOptionalLease(
		state,
		values[jobLeaseOwnerIndex],
		values[jobLeaseTokenIndex],
		leaseFence,
		leaseStartedAtMS,
		leaseExpiresAtMS,
	); err != nil {
		return err
	}
	if state == "leased" {
		if leaseStartedAtMS > updatedAtMS || (leaseDeliveryStarted == 1) != (requestStarts > baseline) {
			return ErrRecordRelation
		}
	} else if leaseDeliveryStarted != 0 {
		return ErrRecordRelation
	}
	if leaseDeliveryStarted == 1 && (deliveryAttempts == 0 || requestStarts == 0 || lastRequestStartedAtMS == 0) {
		return ErrRecordRelation
	}

	activeReservationID := values[jobActiveReservationIDIndex]
	if activeReservationID != "" {
		if _, err := parseNonzeroReservationID(activeReservationID); err != nil {
			return err
		}
		if state != "leased" {
			return ErrRecordRelation
		}
	}
	for _, index := range []int{
		jobActiveStageCommitIDIndex,
		jobLastStageCommitIDIndex,
		jobOutputDigestIndex,
		jobPublicationIDIndex,
		jobCommitIDIndex,
		jobLastTransitionIDIndex,
	} {
		if values[index] != "" {
			if _, err := parseNonzeroDigest(values[index]); err != nil {
				return err
			}
		}
	}
	activeStageCommitID := values[jobActiveStageCommitIDIndex]
	lastStageCommitID := values[jobLastStageCommitIDIndex]
	if (lastStageCommitID == "") != (lastStageFence == 0) {
		return ErrRecordRelation
	}
	// An aborted stage still freezes this fence. Clearing the active pointer
	// cannot permit another reservation or erase the transcript genesis.
	frozen := lastStageFence > 0 && lastStageFence == leaseFence
	if frozen && (requestStarts <= baseline || state == "leased" && (activeReservationID != "" || leaseDeliveryStarted != 1)) {
		return ErrRecordRelation
	}
	if activeStageCommitID != "" {
		if state != "leased" || activeReservationID != "" || activeStageCommitID != lastStageCommitID ||
			lastStageFence != leaseFence || leaseDeliveryStarted != 1 {
			return ErrRecordRelation
		}
	}
	if state != "leased" && activeStageCommitID != "" {
		return ErrRecordRelation
	}
	hasExactAbortTransition := hasExactCurrentAbortTransition(runID, jobID, values, numbers)
	if (values[jobLastTransitionStatusIndex] == string(StatusStageAborted) ||
		state == "leased" && frozen && activeStageCommitID == "") && !hasExactAbortTransition {
		return ErrRecordRelation
	}

	backpressureReason := values[jobCommitBackpressureReasonIndex]
	if !oneOf(backpressureReason, "none", "pages_queue_full", "memory_headroom_low") {
		return ErrInvalidRecordValue
	}
	// The stage-expiry cap can already be elapsed when the first block is
	// recorded. Stage expiry is not present in this hash, so only the durable
	// nonzero deadline and its state/fence authority can be checked here.
	if backpressureReason == "none" {
		if backpressureFence != 0 || backpressureStartedAtMS != 0 || backpressureDeadlineMS != 0 {
			return ErrRecordRelation
		}
	} else if state != "leased" || activeStageCommitID == "" && !hasExactAbortTransition || backpressureFence != leaseFence ||
		backpressureStartedAtMS == 0 || backpressureDeadlineMS == 0 ||
		backpressureDeadlineMS > backpressureStartedAtMS && backpressureDeadlineMS-backpressureStartedAtMS > MaxCommitBackpressureMilliseconds {
		return ErrRecordRelation
	}

	publicationSet := values[jobOutputDigestIndex] != "" || values[jobPublicationIDIndex] != "" ||
		values[jobCommitIDIndex] != "" || values[jobPublishedPageKeyIndex] != ""
	if publicationSet && (values[jobOutputDigestIndex] == "" || values[jobPublicationIDIndex] == "" ||
		values[jobCommitIDIndex] == "" || values[jobPublishedPageKeyIndex] == "") {
		return ErrRecordRelation
	}
	if publicationSet && state != "completed" {
		return ErrRecordRelation
	}
	if state == "completed" {
		if publicationSet {
			publicationID := Digest(values[jobPublicationIDIndex])
			expectedPublicationID, err := DerivePublicationID(PublicationIdentity{
				RunID: runID, JobID: jobID, Fence: Fence(lastStageFence), OutputDigest: Digest(values[jobOutputDigestIndex]),
			})
			if err != nil || publicationID != expectedPublicationID || values[jobCommitIDIndex] != lastStageCommitID ||
				lastStageFence == 0 || lastStageFence != leaseFence || lastDocumentRequestFence != lastStageFence || requestStarts <= baseline ||
				values[jobLastReasonIndex] != string(ReasonPublished) {
				return ErrRecordRelation
			}
			expectedPageKey, err := PageDataKey(publicationID, values[jobLastDocumentTargetURLIndex])
			if err != nil || values[jobPublishedPageKeyIndex] != expectedPageKey {
				return ErrRecordRelation
			}
		} else if values[jobLastReasonIndex] != string(ReasonAlreadyVisited) {
			return ErrRecordRelation
		}
	}
	if values[jobLastTransitionIDIndex] == "" != (values[jobLastTransitionStatusIndex] == "") {
		return ErrRecordRelation
	}
	if values[jobLastTransitionStatusIndex] != "" {
		if _, err := ParseStatus(values[jobLastTransitionStatusIndex]); err != nil {
			return err
		}
	}
	if err := validateJobDispositionRelations(
		state,
		lastReason,
		lastFailureReason,
		Status(values[jobLastTransitionStatusIndex]),
		deliveryAttempts,
		preIORecoveries,
	); err != nil {
		return err
	}

	switch state {
	case "ready", "leased":
		if notBeforeMS != 0 || completedAtMS != 0 || deadAtMS != 0 || cancelledAtMS != 0 {
			return ErrRecordRelation
		}
	case "delayed":
		if notBeforeMS == 0 || completedAtMS != 0 || deadAtMS != 0 || cancelledAtMS != 0 {
			return ErrRecordRelation
		}
	case "completed":
		if notBeforeMS != 0 || completedAtMS == 0 || deadAtMS != 0 || cancelledAtMS != 0 {
			return ErrRecordRelation
		}
	case "dead":
		if notBeforeMS != 0 || completedAtMS != 0 || deadAtMS == 0 || cancelledAtMS != 0 {
			return ErrRecordRelation
		}
	case "cancelled":
		if notBeforeMS != 0 || completedAtMS != 0 || deadAtMS != 0 || cancelledAtMS == 0 {
			return ErrRecordRelation
		}
	}
	if completedAtMS > updatedAtMS || deadAtMS > updatedAtMS || cancelledAtMS > updatedAtMS {
		return ErrRecordRelation
	}
	return nil
}

func hasExactCurrentAbortTransition(runID RunID, jobID JobID, values []string, numbers map[int]uint64) bool {
	leaseFence := numbers[jobLeaseFenceIndex]
	if values[jobStateIndex] != "leased" || values[jobActiveReservationIDIndex] != "" ||
		values[jobActiveStageCommitIDIndex] != "" || values[jobLastStageCommitIDIndex] == "" ||
		numbers[jobLastStageFenceIndex] != leaseFence || numbers[jobLastDocumentRequestFenceIndex] != leaseFence ||
		numbers[jobLeaseDeliveryStartedIndex] != 1 ||
		values[jobLastReasonIndex] != string(ReasonNone) ||
		values[jobLastTransitionStatusIndex] != string(StatusStageAborted) {
		return false
	}
	expectedTransitionID, err := DeriveAbortStageTransitionID(AbortStageTransitionInput{
		Lease: LeaseIdentity{
			RunID:   runID,
			JobID:   jobID,
			OwnerID: OwnerID(values[jobLeaseOwnerIndex]),
			Fence:   Fence(leaseFence),
			Token:   LeaseToken(values[jobLeaseTokenIndex]),
		},
		CommitID: Digest(values[jobLastStageCommitIDIndex]),
	})
	return err == nil && values[jobLastTransitionIDIndex] == string(expectedTransitionID)
}

// A delivery is counted once per fence with any START. Earlier deliveries each
// consumed at least one of B starts; this (possibly cleared) fence contributes
// exactly one iff G>B. Lease clearing cannot erase that contribution.
func validDeliveryAttemptHistory(baseline, generation, attempts uint64) bool {
	if baseline > generation || generation > MaxRequestStartsPerRun || attempts > MaxDeliveryAttempts {
		return false
	}
	priorMinimum, current := uint64(0), uint64(0)
	if baseline > 0 {
		priorMinimum = 1
	}
	if generation > baseline {
		current = 1
	}
	return attempts >= priorMinimum+current && attempts <= baseline+current
}

func validateJobDispositionRelations(
	state string,
	lastReason Reason,
	lastFailureReason Reason,
	lastTransitionStatus Status,
	deliveryAttempts uint64,
	preIORecoveries uint64,
) error {
	preIOExhausted := preIORecoveries == MaxPreIOExpiredLeaseRecoveries
	if preIOExhausted != (state == "dead" && lastReason == ReasonPreIORecoveryExhausted) {
		return ErrRecordRelation
	}
	if lastReason == ReasonRetryExhausted && state != "dead" {
		return ErrRecordRelation
	}

	switch state {
	case "completed":
		if lastReason != ReasonPublished && lastReason != ReasonAlreadyVisited {
			return ErrRecordRelation
		}
		switch lastTransitionStatus {
		case "":
		case StatusCommitted:
			if lastReason != ReasonPublished {
				return ErrRecordRelation
			}
		case StatusVisitedCompleted, StatusCompleted:
			if lastReason != ReasonAlreadyVisited {
				return ErrRecordRelation
			}
		default:
			return ErrRecordRelation
		}
	case "dead":
		if !isDeadLetterReason(lastReason) || lastTransitionStatus != "" && lastTransitionStatus != StatusDead {
			return ErrRecordRelation
		}
		switch lastReason {
		case ReasonRetryExhausted:
			if deliveryAttempts != MaxDeliveryAttempts || !isRetryableReason(lastFailureReason) {
				return ErrRecordRelation
			}
		case ReasonPreIORecoveryExhausted:
			// Recovery's exhaustion disposition is exact, but the normative
			// record contract does not require it to replace retained underlying
			// failure evidence.
			if deliveryAttempts >= MaxDeliveryAttempts ||
				lastFailureReason != ReasonNone && lastFailureReason != ReasonPreIORecoveryExhausted && !isRetryableReason(lastFailureReason) {
				return ErrRecordRelation
			}
		default:
			if lastFailureReason != lastReason {
				return ErrRecordRelation
			}
		}
	case "cancelled":
		if !isCancellationReason(lastReason) || lastTransitionStatus != "" && lastTransitionStatus != StatusCancelled {
			return ErrRecordRelation
		}
	}
	return nil
}

func validateReservationRecordValues(values []string) error {
	reservationID, err := parseNonzeroReservationID(values[reservationIDIndex])
	if err != nil {
		return err
	}
	if _, err := ParseRunID(values[reservationRunIDIndex]); err != nil {
		return err
	}
	if _, err := ParseJobID(values[reservationJobIDIndex]); err != nil {
		return err
	}
	if _, err := ParseOwnerID(values[reservationOwnerIDIndex]); err != nil {
		return err
	}
	if _, err := ParseLeaseToken(values[reservationLeaseTokenIndex]); err != nil {
		return err
	}
	if _, err := ParseFence(values[reservationLeaseFenceIndex]); err != nil {
		return err
	}
	ordinal, err := parsePositiveDecimal(values[reservationRequestOrdinalIndex])
	if err != nil {
		return err
	}
	if ordinal > MaxReservationCreationsPerRun {
		return ErrRecordRelation
	}
	state := values[reservationStateIndex]
	if !oneOf(state, "pending", "started", "finished", "cancelled", "expired") {
		return ErrInvalidRecordValue
	}
	if _, err := ParseRequestKind(values[reservationRequestKindIndex]); err != nil {
		return err
	}
	targetID, err := ParseJobID(values[reservationTargetURLIDIndex])
	if err != nil {
		return err
	}
	canonicalTargetURL := values[reservationCanonicalTargetURLIndex]
	if err := validateJobURL(targetID, canonicalTargetURL); err != nil {
		return err
	}
	targetDigest, err := parseNonzeroDigest(values[reservationTargetDigestIndex])
	if err != nil {
		return err
	}
	expectedTargetDigest, err := DeriveTargetDigest(RequestTarget{URLID: targetID, CanonicalURL: canonicalTargetURL})
	if err != nil || targetDigest != expectedTargetDigest {
		return ErrRecordRelation
	}
	if _, err := parseNonzeroDigest(values[reservationCrawlPolicySHA256Index]); err != nil {
		return err
	}
	// The reservation schema does not carry decision depth. The stored decision
	// digest is therefore lexically validated here and cryptographically bound
	// into the recomputed reservation ID; transitions perform the independent
	// decision-digest comparison with the owning job's depth.
	if _, err := parseNonzeroDigest(values[reservationPolicyDecisionSHA256Index]); err != nil {
		return err
	}
	if _, err := ParseGroupID(values[reservationGroupIDIndex]); err != nil {
		return err
	}
	rateScopeID, err := ParseRateScopeID(values[reservationRateScopeIDIndex])
	if err != nil {
		return err
	}
	globalScopeID, err := parseNonzeroDigest(values[reservationGlobalScopeIDIndex])
	if err != nil {
		return err
	}
	groupScopeID, err := parseNonzeroDigest(values[reservationGroupScopeIDIndex])
	if err != nil {
		return err
	}
	originScopeID, err := parseNonzeroDigest(values[reservationOriginScopeIDIndex])
	if err != nil {
		return err
	}
	expectedGroupScopeID, err := DeriveGroupScopeID(rateScopeID)
	if err != nil {
		return err
	}
	origin, err := DeriveCanonicalOrigin(canonicalTargetURL)
	if err != nil {
		return err
	}
	expectedOriginScopeID, err := DeriveOriginScopeID(origin)
	if err != nil {
		return err
	}
	if globalScopeID != DeriveGlobalScopeID() || groupScopeID != expectedGroupScopeID || originScopeID != expectedOriginScopeID ||
		globalScopeID == groupScopeID || globalScopeID == originScopeID || groupScopeID == originScopeID {
		return ErrRecordRelation
	}

	numbers, err := parseIndexedDecimals(values, []int{
		reservationGlobalConcurrencyIndex,
		reservationGlobalIntervalMSIndex,
		reservationGroupConcurrencyIndex,
		reservationGroupIntervalMSIndex,
		reservationOriginConcurrencyIndex,
		reservationOriginIntervalMSIndex,
		reservationCreatedAtMSIndex,
		reservationStartedAtMSIndex,
		reservationTerminalAtMSIndex,
		reservationDeliveryAttemptsAfterStartIndex,
		reservationJobStartsAfterStartIndex,
		reservationRunStartsAfterStartIndex,
		reservationGroupStartsAfterStartIndex,
		reservationExpiresAtMSIndex,
	})
	if err != nil {
		return err
	}
	globalConcurrency := numbers[reservationGlobalConcurrencyIndex]
	globalIntervalMS := numbers[reservationGlobalIntervalMSIndex]
	groupConcurrency := numbers[reservationGroupConcurrencyIndex]
	groupIntervalMS := numbers[reservationGroupIntervalMSIndex]
	originConcurrency := numbers[reservationOriginConcurrencyIndex]
	originIntervalMS := numbers[reservationOriginIntervalMSIndex]
	createdAtMS := numbers[reservationCreatedAtMSIndex]
	startedAtMS := numbers[reservationStartedAtMSIndex]
	terminalAtMS := numbers[reservationTerminalAtMSIndex]
	deliveryAttemptsAfterStart := numbers[reservationDeliveryAttemptsAfterStartIndex]
	jobStartsAfterStart := numbers[reservationJobStartsAfterStartIndex]
	runStartsAfterStart := numbers[reservationRunStartsAfterStartIndex]
	groupStartsAfterStart := numbers[reservationGroupStartsAfterStartIndex]
	expiresAtMS := numbers[reservationExpiresAtMSIndex]

	if globalConcurrency != GlobalActiveRequestLimit || globalIntervalMS != GlobalScopeIntervalMilliseconds ||
		!validScopeTuple(groupConcurrency, groupIntervalMS) || !validScopeTuple(originConcurrency, originIntervalMS) ||
		groupConcurrency != originConcurrency || groupIntervalMS != originIntervalMS || createdAtMS == 0 || expiresAtMS <= createdAtMS {
		return ErrRecordRelation
	}
	if deliveryAttemptsAfterStart > MaxDeliveryAttempts || deliveryAttemptsAfterStart > jobStartsAfterStart ||
		jobStartsAfterStart > runStartsAfterStart || groupStartsAfterStart > runStartsAfterStart ||
		runStartsAfterStart > MaxRequestStartsPerRun || jobStartsAfterStart > ordinal {
		return ErrRecordRelation
	}
	snapshotsAreZero := deliveryAttemptsAfterStart == 0 && jobStartsAfterStart == 0 && runStartsAfterStart == 0 && groupStartsAfterStart == 0
	snapshotsArePositive := deliveryAttemptsAfterStart > 0 && jobStartsAfterStart > 0 && runStartsAfterStart > 0 && groupStartsAfterStart > 0
	if startedAtMS == 0 && !snapshotsAreZero || startedAtMS != 0 && !snapshotsArePositive {
		return ErrRecordRelation
	}
	if startedAtMS != 0 && (startedAtMS < createdAtMS || startedAtMS >= expiresAtMS) {
		return ErrRecordRelation
	}
	if terminalAtMS != 0 && terminalAtMS < createdAtMS {
		return ErrRecordRelation
	}

	expectedReservationID := deriveReservationRecordID(values)
	if reservationID != expectedReservationID {
		return ErrRecordRelation
	}

	switch state {
	case "pending":
		if startedAtMS != 0 || terminalAtMS != 0 || !snapshotsAreZero {
			return ErrRecordRelation
		}
	case "started":
		if startedAtMS == 0 || terminalAtMS != 0 || snapshotsAreZero {
			return ErrRecordRelation
		}
	case "finished":
		if startedAtMS == 0 || terminalAtMS < startedAtMS || terminalAtMS >= expiresAtMS || snapshotsAreZero {
			return ErrRecordRelation
		}
	case "cancelled":
		if startedAtMS != 0 || terminalAtMS == 0 || terminalAtMS >= expiresAtMS || !snapshotsAreZero {
			return ErrRecordRelation
		}
	case "expired":
		if terminalAtMS < expiresAtMS {
			return ErrRecordRelation
		}
	}
	return nil
}

func validateRateScopeRecordValues(values []string) error {
	scopeID, err := parseNonzeroDigest(values[rateScopeIDIndex])
	if err != nil {
		return err
	}
	numbers, err := parseIndexedDecimals(values, []int{
		rateScopeEffectiveConcurrencyIndex,
		rateScopeEffectiveIntervalMSIndex,
		rateScopeNextAllowedMSIndex,
		rateScopeLastStartedAtMSIndex,
		rateScopeActiveCountIndex,
		rateScopePendingCountIndex,
		rateScopeStartedCountIndex,
		rateScopeUpdatedAtMSIndex,
	})
	if err != nil {
		return err
	}
	effectiveConcurrency := numbers[rateScopeEffectiveConcurrencyIndex]
	effectiveIntervalMS := numbers[rateScopeEffectiveIntervalMSIndex]
	nextAllowedMS := numbers[rateScopeNextAllowedMSIndex]
	lastStartedAtMS := numbers[rateScopeLastStartedAtMSIndex]
	activeCount := numbers[rateScopeActiveCountIndex]
	pendingCount := numbers[rateScopePendingCountIndex]
	startedCount := numbers[rateScopeStartedCountIndex]
	updatedAtMS := numbers[rateScopeUpdatedAtMSIndex]
	if effectiveConcurrency == 0 || effectiveConcurrency > MaxScopeConcurrency || effectiveIntervalMS > MaxScopeIntervalMilliseconds ||
		pendingCount+startedCount != activeCount || updatedAtMS == 0 || lastStartedAtMS > updatedAtMS {
		return ErrRecordRelation
	}
	for _, index := range []int{rateScopeConcurrencySourceSHA256Index, rateScopeIntervalSourceSHA256Index} {
		if _, err := parseNonzeroDigest(values[index]); err != nil {
			return err
		}
	}
	switch values[rateScopeKindIndex] {
	case "global":
		if values[rateScopeWitnessIndex] != "global" || scopeID != DeriveGlobalScopeID() ||
			effectiveConcurrency != GlobalActiveRequestLimit || effectiveIntervalMS != GlobalScopeIntervalMilliseconds || nextAllowedMS != 0 {
			return ErrRecordRelation
		}
	case "group":
		rateScopeID, err := ParseRateScopeID(values[rateScopeWitnessIndex])
		if err != nil {
			return err
		}
		expected, _ := DeriveGroupScopeID(rateScopeID)
		if scopeID != expected {
			return ErrRecordRelation
		}
	case "origin":
		origin := CanonicalOrigin(values[rateScopeWitnessIndex])
		if err := validateCanonicalOrigin(origin); err != nil {
			return err
		}
		expected, _ := DeriveOriginScopeID(origin)
		if scopeID != expected {
			return ErrRecordRelation
		}
	default:
		return ErrInvalidRecordValue
	}
	if values[rateScopeKindIndex] != "global" && lastStartedAtMS != 0 {
		if lastStartedAtMS > MaxExactInteger-effectiveIntervalMS || nextAllowedMS < lastStartedAtMS+effectiveIntervalMS {
			return ErrRecordRelation
		}
	}
	return nil
}

func validateStageMetaRecordValues(values []string) error {
	runID, err := ParseRunID(values[stageRunIDIndex])
	if err != nil {
		return err
	}
	jobID, err := ParseJobID(values[stageJobIDIndex])
	if err != nil {
		return err
	}
	if _, err := ParseOwnerID(values[stageOwnerIDIndex]); err != nil {
		return err
	}
	leaseFence, err := ParseFence(values[stageLeaseFenceIndex])
	if err != nil {
		return err
	}
	for _, index := range []int{stageTokenDigestIndex, stageCommitIDIndex, stagePublicationIDIndex, stageOutputDigestIndex} {
		if _, err := parseNonzeroDigest(values[index]); err != nil {
			return err
		}
	}
	numbers, err := parseIndexedDecimals(values, integerRange(stageRequestStartsBaselineIndex, stageKeyCountIndex))
	if err != nil {
		return err
	}
	if validateRequestStartsTuple(numbers[stageRequestStartsBaselineIndex], numbers[stageRequestStartsGenerationIndex]) != nil {
		return ErrRecordRelation
	}
	createdAtMS := numbers[stageCreatedAtMSIndex]
	expiresAtMS := numbers[stageExpiresAtMSIndex]
	sealedAtMS := numbers[stageSealedAtMSIndex]
	expectedPageFields := numbers[stageExpectedPageFieldsIndex]
	expectedOutlinks := numbers[stageExpectedOutlinksIndex]
	expectedDiscoveries := numbers[stageExpectedDiscoveriesIndex]
	expectedAliases := numbers[stageExpectedAliasesIndex]
	expectedImages := numbers[stageExpectedImagesIndex]
	pageFieldsWritten := numbers[stagePageFieldsWrittenIndex]
	htmlWritten := numbers[stageHTMLWrittenIndex]
	originalHTMLWritten := numbers[stageOriginalHTMLWrittenIndex]
	outlinksWritten := numbers[stageOutlinksWrittenIndex]
	discoveriesWritten := numbers[stageDiscoveriesWrittenIndex]
	aliasesWritten := numbers[stageAliasesWrittenIndex]
	imagesWritten := numbers[stageImagesWrittenIndex]
	manifestWritten := numbers[stageManifestWrittenIndex]
	dataBytes := numbers[stageDataBytesIndex]
	keyCount := numbers[stageKeyCountIndex]

	expectedPublicationID, err := DerivePublicationID(PublicationIdentity{
		RunID: runID, JobID: jobID, Fence: leaseFence, OutputDigest: Digest(values[stageOutputDigestIndex]),
	})
	if err != nil || Digest(values[stagePublicationIDIndex]) != expectedPublicationID {
		return ErrRecordRelation
	}
	if createdAtMS == 0 || createdAtMS > MaxExactInteger-StageTTLMilliseconds || expiresAtMS != createdAtMS+StageTTLMilliseconds ||
		!oneOf(values[stageSealedIndex], "0", "1") ||
		!oneOf(values[stageAbandonedIndex], "0", "1") || htmlWritten > 1 || originalHTMLWritten > 1 || manifestWritten > 1 ||
		expectedPageFields != FinalPageFieldCount || expectedOutlinks > MaxOutlinksPerJob || expectedDiscoveries > MaxDiscoveriesPerJob ||
		expectedAliases == 0 || expectedAliases > MaxAliasesPerJob || expectedImages > MaxImagesPerPage || pageFieldsWritten > expectedPageFields ||
		outlinksWritten > expectedOutlinks || discoveriesWritten > expectedDiscoveries || aliasesWritten > expectedAliases || imagesWritten > expectedImages ||
		dataBytes > MaxStageAggregateLogicalDataBytes || keyCount < 2 || keyCount > MaxStageKeys {
		return ErrRecordRelation
	}
	expectedKeyCount := stageKeyCount(pageFieldsWritten, htmlWritten, originalHTMLWritten, outlinksWritten, discoveriesWritten, aliasesWritten, imagesWritten, manifestWritten)
	if keyCount != expectedKeyCount || (dataBytes == 0) != (keyCount == 2) {
		return ErrRecordRelation
	}
	if values[stageSealedIndex] == "0" && sealedAtMS != 0 ||
		values[stageSealedIndex] == "1" && (sealedAtMS < createdAtMS || sealedAtMS >= expiresAtMS) {
		return ErrRecordRelation
	}
	for index := stagePageFieldsChunkDigestIndex; index < len(values); index++ {
		if values[index] != "" {
			if _, err := parseNonzeroDigest(values[index]); err != nil {
				return err
			}
		}
	}
	if validateStageProgressDigestPresence(
		values,
		pageFieldsWritten,
		htmlWritten,
		originalHTMLWritten,
		outlinksWritten,
		discoveriesWritten,
		aliasesWritten,
		imagesWritten,
		manifestWritten,
	) != nil {
		return ErrRecordRelation
	}
	if values[stageSealedIndex] == "1" {
		// Recovery may mark either an unsealed or sealed stage abandoned without
		// rewriting its stage counters. Sealed completeness remains mandatory in
		// both cases and is the complete lifecycle relation available in this hash.
		if pageFieldsWritten != expectedPageFields || htmlWritten != 1 || originalHTMLWritten != 1 ||
			outlinksWritten != expectedOutlinks || discoveriesWritten != expectedDiscoveries || aliasesWritten != expectedAliases ||
			imagesWritten != expectedImages || manifestWritten != 1 || dataBytes == 0 ||
			validateSealedStageDigestPresence(values, expectedOutlinks, expectedDiscoveries, expectedImages) != nil {
			return ErrRecordRelation
		}
	}
	return nil
}

func validateStageProgressDigestPresence(
	values []string,
	pageFieldsWritten uint64,
	htmlWritten uint64,
	originalHTMLWritten uint64,
	outlinksWritten uint64,
	discoveriesWritten uint64,
	aliasesWritten uint64,
	imagesWritten uint64,
	manifestWritten uint64,
) error {
	if pageFieldsWritten < htmlWritten+originalHTMLWritten {
		return ErrRecordRelation
	}
	nonBlobPageFields := pageFieldsWritten - htmlWritten - originalHTMLWritten
	if nonBlobPageFields != 0 && nonBlobPageFields != FinalPageFieldCount-2 ||
		(values[stagePageFieldsChunkDigestIndex] != "") != (nonBlobPageFields != 0) ||
		(values[stageHTMLChunkDigestIndex] != "") != (htmlWritten != 0) ||
		(values[stageOriginalHTMLChunkDigestIndex] != "") != (originalHTMLWritten != 0) {
		return ErrRecordRelation
	}
	if validateStageBatchDigestProgress(values, []int{
		stageOutlinksChunk0DigestIndex,
		stageOutlinksChunk1DigestIndex,
		stageOutlinksChunk2DigestIndex,
		stageOutlinksChunk3DigestIndex,
	}, outlinksWritten) != nil {
		return ErrRecordRelation
	}
	if validateStageBatchDigestProgress(values, []int{
		stageDiscoveriesChunk0DigestIndex,
		stageDiscoveriesChunk1DigestIndex,
	}, discoveriesWritten) != nil {
		return ErrRecordRelation
	}
	if (values[stageAliasesChunk0DigestIndex] != "") != (aliasesWritten != 0) ||
		(values[stageImagesChunk0DigestIndex] != "") != (imagesWritten != 0) ||
		(values[stageManifestChunkDigestIndex] != "") != (manifestWritten != 0) {
		return ErrRecordRelation
	}
	return nil
}

func validateStageBatchDigestProgress(values []string, digestIndexes []int, written uint64) error {
	digestCount := uint64(0)
	sawEmpty := false
	for _, index := range digestIndexes {
		if values[index] == "" {
			sawEmpty = true
			continue
		}
		if sawEmpty {
			return ErrRecordRelation
		}
		digestCount++
	}
	if written == 0 {
		if digestCount != 0 {
			return ErrRecordRelation
		}
		return nil
	}
	if digestCount == 0 || written < digestCount || written > digestCount*MaxNonBlobStageBatchRecords {
		return ErrRecordRelation
	}
	return nil
}

func deriveReservationRecordID(values []string) ReservationID {
	digest := digestFramed(
		"mifolyo:request-reservation:v2",
		[]byte(values[reservationRunIDIndex]),
		[]byte(values[reservationJobIDIndex]),
		[]byte(values[reservationLeaseFenceIndex]),
		[]byte(values[reservationLeaseTokenIndex]),
		[]byte(values[reservationRequestOrdinalIndex]),
		[]byte(values[reservationRequestKindIndex]),
		[]byte(values[reservationTargetURLIDIndex]),
		[]byte(values[reservationTargetDigestIndex]),
		[]byte(values[reservationCrawlPolicySHA256Index]),
		[]byte(values[reservationPolicyDecisionSHA256Index]),
		[]byte(values[reservationGroupIDIndex]),
		[]byte(values[reservationRateScopeIDIndex]),
		[]byte(values[reservationGlobalScopeIDIndex]),
		[]byte(values[reservationGroupScopeIDIndex]),
		[]byte(values[reservationOriginScopeIDIndex]),
		[]byte(values[reservationGlobalConcurrencyIndex]),
		[]byte(values[reservationGlobalIntervalMSIndex]),
		[]byte(values[reservationGroupConcurrencyIndex]),
		[]byte(values[reservationGroupIntervalMSIndex]),
		[]byte(values[reservationOriginConcurrencyIndex]),
		[]byte(values[reservationOriginIntervalMSIndex]),
	)
	return ReservationID(digest)
}

func validateSealedStageDigestPresence(values []string, expectedOutlinks, expectedDiscoveries, expectedImages uint64) error {
	for _, index := range []int{
		stagePageFieldsChunkDigestIndex,
		stageHTMLChunkDigestIndex,
		stageOriginalHTMLChunkDigestIndex,
		stageAliasesChunk0DigestIndex,
		stageManifestChunkDigestIndex,
	} {
		if values[index] == "" {
			return ErrRecordRelation
		}
	}

	outlinkChunks := stageChunkCount(expectedOutlinks)
	for ordinal, index := range []int{
		stageOutlinksChunk0DigestIndex,
		stageOutlinksChunk1DigestIndex,
		stageOutlinksChunk2DigestIndex,
		stageOutlinksChunk3DigestIndex,
	} {
		if (values[index] != "") != (uint64(ordinal) < outlinkChunks) {
			return ErrRecordRelation
		}
	}
	discoveryChunks := stageChunkCount(expectedDiscoveries)
	for ordinal, index := range []int{stageDiscoveriesChunk0DigestIndex, stageDiscoveriesChunk1DigestIndex} {
		if (values[index] != "") != (uint64(ordinal) < discoveryChunks) {
			return ErrRecordRelation
		}
	}
	if (values[stageImagesChunk0DigestIndex] != "") != (expectedImages != 0) {
		return ErrRecordRelation
	}
	return nil
}

func stageChunkCount(recordCount uint64) uint64 {
	if recordCount == 0 {
		return 0
	}
	return (recordCount + MaxNonBlobStageBatchRecords - 1) / MaxNonBlobStageBatchRecords
}

func stageKeyCount(pageFieldsWritten, htmlWritten, originalHTMLWritten, outlinksWritten, discoveriesWritten, aliasesWritten, imagesWritten, manifestWritten uint64) uint64 {
	count := uint64(2) // T:meta and T:keys exist from begin.
	if pageFieldsWritten != 0 || htmlWritten != 0 || originalHTMLWritten != 0 {
		count++
	}
	if outlinksWritten != 0 {
		count++
	}
	if discoveriesWritten != 0 {
		count += 3
	}
	if aliasesWritten != 0 {
		count++
	}
	count += imagesWritten
	if manifestWritten != 0 {
		count++
	}
	return count
}

func validateOptionalDocumentIdentity(startedAt, fence, targetID, targetURL, targetDigest string) error {
	allEmpty := startedAt == "0" && fence == "0" && targetID == "" && targetURL == "" && targetDigest == ""
	if allEmpty {
		return nil
	}
	if startedAt == "0" || fence == "0" || targetID == "" || targetURL == "" || targetDigest == "" {
		return ErrRecordRelation
	}
	if _, err := parsePositiveDecimal(startedAt); err != nil {
		return err
	}
	if _, err := ParseFence(fence); err != nil {
		return err
	}
	jobID, err := ParseJobID(targetID)
	if err != nil {
		return err
	}
	if err := validateJobURL(jobID, targetURL); err != nil {
		return err
	}
	digest, err := DeriveTargetDigest(RequestTarget{URLID: jobID, CanonicalURL: targetURL})
	if err != nil || string(digest) != targetDigest {
		return ErrRecordRelation
	}
	return nil
}

func validateOptionalLease(state, owner, token string, fence, startedAt, expiresAt uint64) error {
	if state == "leased" {
		if _, err := ParseOwnerID(owner); err != nil {
			return err
		}
		if _, err := ParseLeaseToken(token); err != nil {
			return err
		}
		if fence == 0 || startedAt == 0 || expiresAt <= startedAt {
			return ErrRecordRelation
		}
		return nil
	}
	if owner != "" || token != "" || startedAt != 0 || expiresAt != 0 {
		return ErrRecordRelation
	}
	return nil
}

func parseIndexedDecimals(values []string, indexes []int) (map[int]uint64, error) {
	parsed := make(map[int]uint64, len(indexes))
	for _, index := range indexes {
		if index < 0 || index >= len(values) {
			return nil, ErrInvalidSchema
		}
		value, err := parseCanonicalDecimal(values[index])
		if err != nil {
			return nil, err
		}
		parsed[index] = value
	}
	return parsed, nil
}

func integerRange(first, last int) []int {
	result := make([]int, 0, last-first+1)
	for index := first; index <= last; index++ {
		result = append(result, index)
	}
	return result
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func validateBoundedUTF8(value string, maximum int, allowEmpty bool) error {
	if (!allowEmpty && value == "") || len(value) > maximum || !utf8.ValidString(value) || strings.IndexFunc(value, func(r rune) bool { return r < 0x20 }) >= 0 {
		return ErrInvalidRecordValue
	}
	return nil
}
