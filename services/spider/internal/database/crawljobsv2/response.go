package crawljobsv2

import (
	"errors"
	"strconv"
	"unicode/utf8"
)

var (
	ErrResponseNotArray         = errors.New("crawljobsv2: Redis response is not an array")
	ErrResponseArity            = errors.New("crawljobsv2: Redis response has invalid arity")
	ErrResponseScalarType       = errors.New("crawljobsv2: Redis response contains a non-bulk scalar")
	ErrInvalidResponseText      = errors.New("crawljobsv2: Redis response contains invalid text")
	ErrInvalidRedisMillis       = errors.New("crawljobsv2: invalid Redis millisecond value")
	ErrResponseStatus           = errors.New("crawljobsv2: status is invalid for operation")
	ErrResponseScalar           = errors.New("crawljobsv2: response scalar is invalid")
	ErrInvalidResponseAuthority = errors.New("crawljobsv2: response does not authorize request I/O")
)

type RedisMilliseconds uint64

type ResponseSchema struct {
	Operation OperationName
	Status    Status
	Fields    []string
}

type StartRequestStarted struct {
	reservationID      ReservationID
	startedAtMS        RedisMilliseconds
	deliveryAttempts   uint64
	jobRequestStarts   uint64
	runRequestStarts   uint64
	groupRequestStarts uint64
	ioPermission       bool
}

type StartRequestRateBlocked struct {
	scopeID       Digest
	nextAllowedMS RedisMilliseconds
	afterIO       bool
}

type LeaseLostResponse struct {
	currentFence uint64
}

// StartRequestResponse is a closed union. Exactly one details pointer is set
// for STARTED/ALREADY_STARTED, RATE_BLOCKED, or LEASE_LOST; definitive
// authorization and cancellation responses have no detail payload.
type StartRequestResponse struct {
	status                Status
	nowMS                 RedisMilliseconds
	expectedReservationID ReservationID
	intent                ReservationIntent
	started               *StartRequestStarted
	rateBlocked           *StartRequestRateBlocked
	leaseLost             *LeaseLostResponse
	initialized           bool
}

// RequestIOPermit is the only value that authorizes DNS or request I/O. Its
// fields are private so it can only be obtained from an intent-bound Redis
// STARTED/ALREADY_STARTED response carrying io_permission=1.
type RequestIOPermit struct {
	intent        ReservationIntent
	reservationID ReservationID
	started       StartRequestStarted
	initialized   bool
}

func (response StartRequestResponse) Status() Status { return response.status }

func (response StartRequestResponse) NowMS() RedisMilliseconds { return response.nowMS }

func (response StartRequestResponse) StartedDetails() (StartRequestStarted, bool) {
	if response.started == nil {
		return StartRequestStarted{}, false
	}
	return *response.started, true
}

func (response StartRequestResponse) RateBlockedDetails() (StartRequestRateBlocked, bool) {
	if response.rateBlocked == nil {
		return StartRequestRateBlocked{}, false
	}
	return *response.rateBlocked, true
}

func (response StartRequestResponse) LeaseLostDetails() (LeaseLostResponse, bool) {
	if response.leaseLost == nil {
		return LeaseLostResponse{}, false
	}
	return *response.leaseLost, true
}

func (started StartRequestStarted) ReservationID() ReservationID   { return started.reservationID }
func (started StartRequestStarted) StartedAtMS() RedisMilliseconds { return started.startedAtMS }
func (started StartRequestStarted) DeliveryAttempts() uint64       { return started.deliveryAttempts }
func (started StartRequestStarted) JobRequestStarts() uint64       { return started.jobRequestStarts }
func (started StartRequestStarted) RunRequestStarts() uint64       { return started.runRequestStarts }
func (started StartRequestStarted) GroupRequestStarts() uint64     { return started.groupRequestStarts }
func (started StartRequestStarted) IOPermission() bool             { return started.ioPermission }
func (blocked StartRequestRateBlocked) ScopeID() Digest            { return blocked.scopeID }
func (blocked StartRequestRateBlocked) NextAllowedMS() RedisMilliseconds {
	return blocked.nextAllowedMS
}
func (blocked StartRequestRateBlocked) AfterIO() bool { return blocked.afterIO }
func (lost LeaseLostResponse) CurrentFence() uint64   { return lost.currentFence }

func (response StartRequestResponse) IOPermit() (RequestIOPermit, error) {
	if !response.initialized || response.started == nil || !response.started.ioPermission ||
		response.started.reservationID != response.expectedReservationID {
		return RequestIOPermit{}, ErrInvalidResponseAuthority
	}
	return RequestIOPermit{
		intent:        response.intent,
		reservationID: response.expectedReservationID,
		started:       *response.started,
		initialized:   true,
	}, nil
}

type parsedResponse struct {
	status Status
	nowMS  RedisMilliseconds
	tail   []string
}

func ResponseSchemaFor(operation OperationName, status Status) (ResponseSchema, error) {
	if _, err := ParseOperationName(string(operation)); err != nil {
		return ResponseSchema{}, err
	}
	if _, err := ParseStatus(string(status)); err != nil {
		return ResponseSchema{}, err
	}
	tail, err := responseTailFields(operation, status)
	if err != nil {
		return ResponseSchema{}, err
	}
	fields := make([]string, 0, 2+len(tail))
	fields = append(fields, "status", "now_ms")
	fields = append(fields, tail...)
	return ResponseSchema{Operation: operation, Status: status, Fields: fields}, nil
}

// ValidateOperationResponse validates an exact operation/status schema without
// exposing its potentially sensitive raw tail.
func ValidateOperationResponse(operation OperationName, raw any) error {
	_, err := parseOperationResponse(operation, raw)
	return err
}

func ParseStartRequestResponse(intent ReservationIntent, raw any) (StartRequestResponse, error) {
	expectedReservationID, err := DeriveReservationID(intent)
	if err != nil {
		return StartRequestResponse{}, err
	}
	parsed, err := parseOperationResponse(OperationStartRequest, raw)
	if err != nil {
		return StartRequestResponse{}, err
	}
	response := StartRequestResponse{
		status: parsed.status, nowMS: parsed.nowMS, expectedReservationID: expectedReservationID,
		intent: intent, initialized: true,
	}
	switch parsed.status {
	case StatusStarted, StatusAlreadyStarted:
		reservationID, err := ParseReservationID(parsed.tail[0])
		if err != nil || reservationID != expectedReservationID {
			return StartRequestResponse{}, ErrResponseScalar
		}
		startedAt, err := parseResponseUint(parsed.tail[1])
		if err != nil || startedAt == 0 {
			return StartRequestResponse{}, ErrResponseScalar
		}
		deliveryAttempts, err := parseResponseUint(parsed.tail[2])
		if err != nil || deliveryAttempts == 0 || deliveryAttempts > MaxDeliveryAttempts {
			return StartRequestResponse{}, ErrResponseScalar
		}
		jobStarts, err := parseResponseUint(parsed.tail[3])
		if err != nil || jobStarts == 0 || jobStarts > MaxRequestStartsPerRun {
			return StartRequestResponse{}, ErrResponseScalar
		}
		runStarts, err := parseResponseUint(parsed.tail[4])
		if err != nil || runStarts == 0 || runStarts > MaxRequestStartsPerRun {
			return StartRequestResponse{}, ErrResponseScalar
		}
		groupStarts, err := parseResponseUint(parsed.tail[5])
		if err != nil || groupStarts == 0 || groupStarts > MaxRequestStartsPerGroup {
			return StartRequestResponse{}, ErrResponseScalar
		}
		permission, err := parseResponseBool(parsed.tail[6])
		if err != nil {
			return StartRequestResponse{}, err
		}
		response.started = &StartRequestStarted{
			reservationID: reservationID, startedAtMS: RedisMilliseconds(startedAt),
			deliveryAttempts: deliveryAttempts, jobRequestStarts: jobStarts,
			runRequestStarts: runStarts, groupRequestStarts: groupStarts,
			ioPermission: permission,
		}
	case StatusRateBlocked:
		scopeID, err := ParseDigest(parsed.tail[0])
		if err != nil || scopeID != intent.Decision.GlobalScopeID && scopeID != intent.Decision.GroupScopeID && scopeID != intent.Decision.OriginScopeID {
			return StartRequestResponse{}, ErrResponseScalar
		}
		nextAllowed, err := parseResponseUint(parsed.tail[1])
		if err != nil {
			return StartRequestResponse{}, err
		}
		afterIO, err := parseResponseBool(parsed.tail[2])
		if err != nil {
			return StartRequestResponse{}, err
		}
		response.rateBlocked = &StartRequestRateBlocked{
			scopeID: scopeID, nextAllowedMS: RedisMilliseconds(nextAllowed), afterIO: afterIO,
		}
	case StatusLeaseLost:
		currentFence, err := parseResponseUint(parsed.tail[0])
		if err != nil {
			return StartRequestResponse{}, err
		}
		response.leaseLost = &LeaseLostResponse{currentFence: currentFence}
	case StatusAuthorizationExpired, StatusRunCancelled:
		// The exact response has no operation-specific fields.
	default:
		return StartRequestResponse{}, ErrResponseStatus
	}
	return response, nil
}

func parseOperationResponse(operation OperationName, raw any) (parsedResponse, error) {
	values, ok := responseArray(raw)
	if !ok {
		return parsedResponse{}, ErrResponseNotArray
	}
	if len(values) < 2 || len(values) > MaxResponseEnvelopeScalars {
		return parsedResponse{}, ErrResponseArity
	}
	scalars := make([]string, len(values))
	for index, value := range values {
		scalar, ok := value.(string)
		if !ok {
			return parsedResponse{}, ErrResponseScalarType
		}
		if !utf8.ValidString(scalar) {
			return parsedResponse{}, ErrInvalidResponseText
		}
		scalars[index] = scalar
	}
	status, err := ParseStatus(scalars[0])
	if err != nil {
		return parsedResponse{}, err
	}
	schema, err := ResponseSchemaFor(operation, status)
	if err != nil {
		return parsedResponse{}, err
	}
	if len(scalars) != len(schema.Fields) {
		return parsedResponse{}, ErrResponseArity
	}
	now, err := parseResponseUint(scalars[1])
	if err != nil {
		return parsedResponse{}, ErrInvalidRedisMillis
	}
	for index, field := range schema.Fields[2:] {
		if err := validateResponseField(field, scalars[index+2], status); err != nil {
			return parsedResponse{}, err
		}
	}
	if err := validateResponseRelations(operation, status, RedisMilliseconds(now), scalars[2:]); err != nil {
		return parsedResponse{}, err
	}
	return parsedResponse{
		status: status,
		nowMS:  RedisMilliseconds(now),
		tail:   append([]string(nil), scalars[2:]...),
	}, nil
}

func validateResponseRelations(operation OperationName, status Status, nowMS RedisMilliseconds, tail []string) error {
	switch operation {
	case OperationEnqueueBatch:
		newJobs, err := parseResponseUint(tail[0])
		if err != nil {
			return err
		}
		reconciledJobs, err := parseResponseUint(tail[1])
		if err != nil {
			return err
		}
		jobCount, err := parseResponseUint(tail[2])
		if err != nil || newJobs > FeederEnqueueBatchSize || reconciledJobs > FeederEnqueueBatchSize ||
			newJobs > FeederEnqueueBatchSize-reconciledJobs || jobCount > MaxJobsPerRun {
			return ErrResponseScalar
		}
		if revision, err := parseResponseUint(tail[3]); err != nil || revision == 0 {
			return ErrResponseScalar
		}
	case OperationBeginRunAudit:
		revision, err := parseResponseUint(tail[0])
		jobCount, countErr := parseResponseUint(tail[1])
		if err != nil || revision == 0 || countErr != nil || jobCount > MaxJobsPerRun {
			return ErrResponseScalar
		}
	case OperationAuditRunBatch:
		checked, err := parseResponseUint(tail[0])
		auditCount, countErr := parseResponseUint(tail[1])
		if err != nil || checked > RunAuditBatchSize || countErr != nil || auditCount > MaxJobsPerRun ||
			status == StatusBatchMore && tail[2] == "" || status == StatusBatchDone && tail[2] != "" {
			return ErrResponseScalar
		}
	case OperationSealRun:
		if jobCount, err := parseResponseUint(tail[0]); err != nil || jobCount > MaxJobsPerRun {
			return ErrResponseScalar
		}
	case OperationActivateRun:
		if err := validatePastResponseTime(nowMS, tail[0]); err != nil {
			return err
		}
	case OperationRejectReady:
		if status == StatusDead {
			return ValidateTransitionReason(operation, Reason(tail[0]))
		}
	case OperationTryClaim:
		switch status {
		case StatusClaimed, StatusAlreadyClaimed:
			leaseExpiry, err := parseResponseUint(tail[1])
			if err != nil || leaseExpiry <= uint64(nowMS) || leaseExpiry > uint64(nowMS)+LeaseTTLMilliseconds ||
				status == StatusClaimed && leaseExpiry != uint64(nowMS)+LeaseTTLMilliseconds || tail[3] != tail[1] {
				return ErrResponseScalar
			}
		case StatusVisitedCompleted:
			if err := validatePastResponseTime(nowMS, tail[0]); err != nil {
				return err
			}
		case StatusCapacityBlocked, StatusRateBlocked, StatusRunBudgetExhausted,
			StatusRunReservationLimitExhausted, StatusGroupBudgetExhausted:
			if err := validateBlockedResponse(operation, status, nowMS, tail); err != nil {
				return err
			}
			if tail[len(tail)-1] != "0" {
				return ErrResponseScalar
			}
		case StatusLeaseCapacityBlocked:
			if tail[1] != canonicalDecimal(MaxActiveLeases) {
				return ErrResponseScalar
			}
			active, err := parseResponseUint(tail[0])
			if err != nil || active != MaxActiveLeases {
				return ErrResponseScalar
			}
		case StatusStageCapacityBlocked:
			if tail[0] != string(BlockedStageSlotsFull) || tail[2] != canonicalDecimal(MaxStageSlots) {
				return ErrResponseScalar
			}
			active, err := parseResponseUint(tail[1])
			if err != nil || active != MaxStageSlots {
				return ErrResponseScalar
			}
		}
	case OperationRenewLease:
		if status == StatusRenewed {
			expiresAt, err := parseResponseUint(tail[0])
			if err != nil || expiresAt != uint64(nowMS)+LeaseTTLMilliseconds {
				return ErrResponseScalar
			}
		}
	case OperationReserveRequest:
		switch status {
		case StatusReserved, StatusAlreadyReserved:
			expiresAt, err := parseResponseUint(tail[1])
			if err != nil || expiresAt <= uint64(nowMS) || expiresAt > uint64(nowMS)+LeaseTTLMilliseconds {
				return ErrResponseScalar
			}
		case StatusCapacityBlocked, StatusRateBlocked, StatusRunBudgetExhausted,
			StatusRunReservationLimitExhausted, StatusGroupBudgetExhausted:
			if err := validateBlockedResponse(operation, status, nowMS, tail); err != nil {
				return err
			}
		}
	case OperationStartRequest:
		if status == StatusStarted || status == StatusAlreadyStarted {
			startedAt, err := parseResponseUint(tail[1])
			if err != nil || startedAt == 0 || startedAt > uint64(nowMS) || status == StatusStarted && startedAt != uint64(nowMS) {
				return ErrResponseScalar
			}
			deliveryAttempts, err := parseResponseUint(tail[2])
			if err != nil || deliveryAttempts == 0 || deliveryAttempts > MaxDeliveryAttempts {
				return ErrResponseScalar
			}
			jobStarts, err := parseResponseUint(tail[3])
			if err != nil || jobStarts == 0 || jobStarts > MaxRequestStartsPerRun || deliveryAttempts > jobStarts {
				return ErrResponseScalar
			}
			runStarts, err := parseResponseUint(tail[4])
			if err != nil || runStarts == 0 || runStarts > MaxRequestStartsPerRun || jobStarts > runStarts {
				return ErrResponseScalar
			}
			groupStarts, err := parseResponseUint(tail[5])
			if err != nil || groupStarts == 0 || groupStarts > MaxRequestStartsPerGroup || groupStarts > runStarts {
				return ErrResponseScalar
			}
			if status == StatusStarted && tail[6] != "1" {
				return ErrResponseScalar
			}
		} else if status == StatusRateBlocked {
			if err := validateBlockedResponse(operation, status, nowMS, tail); err != nil {
				return err
			}
		}
	case OperationReleaseBeforeIO:
		if status == StatusReleasedReady {
			if err := validatePastResponseTime(nowMS, tail[0]); err != nil {
				return err
			}
		}
	case OperationRetry:
		switch status {
		case StatusRetryScheduled:
			notBefore, notBeforeErr := parseResponseUint(tail[0])
			deliveryAttempts, err := parseResponseUint(tail[1])
			delay := RetryDelayAttempt1Milliseconds
			if deliveryAttempts == 2 {
				delay = RetryDelayAttempt2Milliseconds
			}
			if err != nil || deliveryAttempts == 0 || deliveryAttempts >= MaxDeliveryAttempts ||
				notBeforeErr != nil || notBefore != uint64(nowMS)+delay {
				return ErrResponseScalar
			}
			if err := ValidateTransitionReason(operation, Reason(tail[2])); err != nil {
				return err
			}
		case StatusDead:
			if err := validatePastResponseTime(nowMS, tail[0]); err != nil {
				return err
			}
			if err := ValidateTransitionReason(operation, Reason(tail[2])); err != nil {
				return err
			}
		case StatusCancelled:
			if err := validatePastResponseTime(nowMS, tail[0]); err != nil {
				return err
			}
			if !isCancellationReason(Reason(tail[1])) {
				return ErrResponseScalar
			}
		}
	case OperationDead:
		if status != StatusLeaseLost {
			return validateTerminalResponseReason(operation, status, nowMS, tail)
		}
	case OperationCancelJob:
		if status != StatusLeaseLost {
			return validateTerminalResponseReason(operation, status, nowMS, tail)
		}
	case OperationCompleteNoOutput:
		if status != StatusLeaseLost {
			return validateTerminalResponseReason(operation, status, nowMS, tail)
		}
	case OperationBeginStage:
		if status == StatusStageBegun || status == StatusExistsIdentical {
			expiresAt, err := parseResponseUint(tail[1])
			remaining, remainingErr := parseResponseUint(tail[2])
			if err != nil || expiresAt <= uint64(nowMS) || expiresAt > uint64(nowMS)+StageTTLMilliseconds ||
				status == StatusStageBegun && expiresAt != uint64(nowMS)+StageTTLMilliseconds ||
				remainingErr != nil || remaining < StageControlReservationBytes || remaining > StageMemoryReservationBytes {
				return ErrResponseScalar
			}
		} else if status == StatusStageCapacityBlocked {
			blocked := BlockedReason(tail[0])
			if blocked != BlockedStageSlotsFull && blocked != BlockedMemoryHeadroomLow {
				return ErrResponseScalar
			}
			if tail[2] != canonicalDecimal(MaxStageSlots) {
				return ErrResponseScalar
			}
			active, err := parseResponseUint(tail[1])
			if err != nil || active > MaxStageSlots || blocked == BlockedStageSlotsFull && active != MaxStageSlots {
				return ErrResponseScalar
			}
		}
	case OperationStagePageFields, OperationStagePageBlob, OperationStageOutlinksBatch,
		OperationStageDiscoveriesBatch, OperationStageAliasesBatch, OperationStageImagesBatch,
		OperationStageImageManifest:
		if status == StatusStaged || status == StatusExistsIdentical {
			return validateStageResponse(operation, tail)
		}
	case OperationCommit:
		if status == StatusCommitted || status == StatusAlreadyCommitted {
			if err := validatePastResponseTime(nowMS, tail[2]); err != nil {
				return err
			}
			if status == StatusCommitted && tail[2] != canonicalDecimal(uint64(nowMS)) {
				return ErrResponseScalar
			}
		} else if status == StatusDownstreamBackpressure {
			blocked := BlockedReason(tail[0])
			if blocked != BlockedPagesQueueFull && blocked != BlockedMemoryHeadroomLow {
				return ErrResponseScalar
			}
			startedAt, err := parseResponseUint(tail[1])
			deadline, deadlineErr := parseResponseUint(tail[2])
			if err != nil || startedAt == 0 || startedAt > uint64(nowMS) || deadlineErr != nil || deadline == 0 ||
				deadline > startedAt && deadline-startedAt > MaxCommitBackpressureMilliseconds {
				return ErrResponseScalar
			}
		}
	case OperationPromoteDue, OperationRecoverExpired, OperationCancelBatch, OperationMaintainRateScopes:
		if err := validateMaintenanceBatchResponse(tail, MaintenanceBatchSize); err != nil {
			return err
		}
	case OperationCleanStage:
		if err := validateMaintenanceBatchResponse(tail, 1); err != nil {
			return err
		}
	case OperationCancelRun:
		if err := validatePastResponseTime(nowMS, tail[0]); err != nil {
			return err
		}
		if reason := Reason(tail[1]); reason != ReasonOperatorCancelled && reason != ReasonSourceCancelled && reason != ReasonAuthorizationExpired {
			return ErrResponseScalar
		}
	case OperationFinalizeRun:
		if !validFinalizeReason(status, Reason(tail[1])) {
			return ErrResponseScalar
		}
		if status == StatusNotDue {
			if tail[0] != "0" {
				return ErrResponseScalar
			}
		} else if err := validatePastResponseTime(nowMS, tail[0]); err != nil {
			return err
		}
	case OperationArchiveRun:
		if status == StatusArchived || status == StatusExistsIdentical {
			if err := validatePastResponseTime(nowMS, tail[0]); err != nil {
				return err
			}
		} else if status == StatusNotDue {
			if err := validateFutureResponseTime(nowMS, tail[0]); err != nil {
				return err
			}
		}
	case OperationPurgeRunBatch:
		if status == StatusBatchMore || status == StatusPurged {
			if err := validateMaintenanceBatchResponse(tail, MaintenanceBatchSize); err != nil {
				return err
			}
		} else if status == StatusNotDue {
			return validateFutureResponseTime(nowMS, tail[0])
		}
	case OperationSealStage:
		if status == StatusSealed || status == StatusExistsIdentical {
			dataBytes, err := parseResponseUint(tail[1])
			keyCount, keyErr := parseResponseUint(tail[2])
			if err != nil || dataBytes > MaxStageAggregateLogicalDataBytes || keyErr != nil || keyCount < 5 || keyCount > MaxStageKeys {
				return ErrResponseScalar
			}
		}
	case OperationAbortStage:
		if status == StatusStageAborted || status == StatusExistsIdentical {
			unlinkedKeys, err := parseResponseUint(tail[1])
			if err != nil || unlinkedKeys == 0 || unlinkedKeys > MaxStageKeys {
				return ErrResponseScalar
			}
		}
	}
	return nil
}

func validatePastResponseTime(nowMS RedisMilliseconds, value string) error {
	timestamp, err := parseResponseUint(value)
	if err != nil || timestamp == 0 || timestamp > uint64(nowMS) {
		return ErrResponseScalar
	}
	return nil
}

func validateFutureResponseTime(nowMS RedisMilliseconds, value string) error {
	timestamp, err := parseResponseUint(value)
	if err != nil || timestamp <= uint64(nowMS) {
		return ErrResponseScalar
	}
	return nil
}

func validateMaintenanceBatchResponse(tail []string, maximum uint64) error {
	processed, err := parseResponseUint(tail[0])
	if err != nil || processed > maximum {
		return ErrResponseScalar
	}
	return nil
}

func validateBlockedResponse(operation OperationName, status Status, nowMS RedisMilliseconds, tail []string) error {
	switch status {
	case StatusCapacityBlocked:
		active, err := parseResponseUint(tail[1])
		if err != nil {
			return err
		}
		concurrency, err := parseResponseUint(tail[2])
		if err != nil || concurrency == 0 || concurrency > MaxScopeConcurrency || active < concurrency {
			return ErrResponseScalar
		}
	case StatusRateBlocked:
		nextAllowed, err := parseResponseUint(tail[1])
		// Claim and reserve may be blocked by an unrecovered pending member
		// whose logical expiry is already due. Start has no such branch and can
		// report RATE_BLOCKED only for a future current-interval deadline.
		if err != nil || nextAllowed == 0 || operation == OperationStartRequest && nextAllowed <= uint64(nowMS) {
			return ErrResponseScalar
		}
	case StatusRunBudgetExhausted:
		return validateBudgetBlocked(tail[0], tail[1], tail[2], MaxRequestStartsPerRun)
	case StatusRunReservationLimitExhausted:
		created, err := parseResponseUint(tail[0])
		if err != nil || created != MaxReservationCreationsPerRun || tail[1] != canonicalDecimal(MaxReservationCreationsPerRun) {
			return ErrResponseScalar
		}
	case StatusGroupBudgetExhausted:
		return validateBudgetBlocked(tail[1], tail[2], tail[3], MaxRequestStartsPerGroup)
	default:
		return ErrResponseStatus
	}
	return nil
}

func validateBudgetBlocked(startedText, pendingText, limitText string, maximum uint64) error {
	started, err := parseResponseUint(startedText)
	if err != nil {
		return err
	}
	pending, err := parseResponseUint(pendingText)
	if err != nil {
		return err
	}
	limit, err := parseResponseUint(limitText)
	if err != nil || limit == 0 || limit > maximum || started > MaxExactInteger-pending || started+pending != limit {
		return ErrResponseScalar
	}
	return nil
}

func validateTerminalResponseReason(operation OperationName, status Status, nowMS RedisMilliseconds, tail []string) error {
	// Terminal timestamps are immutable transition times, so exact replays may
	// return a value earlier than this invocation's Redis TIME.
	if err := validatePastResponseTime(nowMS, tail[0]); err != nil {
		return err
	}
	reason := Reason(tail[1])
	if status == StatusCancelled {
		if !isCancellationReason(reason) {
			return ErrResponseScalar
		}
		return nil
	}
	if err := ValidateTransitionReason(operation, reason); err != nil {
		return err
	}
	return nil
}

func validateStageResponse(operation OperationName, tail []string) error {
	kind := ChunkKind(tail[1])
	expectedKind := ChunkKind("")
	switch operation {
	case OperationStagePageFields:
		expectedKind = ChunkPageFields
	case OperationStagePageBlob:
		if kind != ChunkHTML && kind != ChunkOriginalHTML {
			return ErrResponseScalar
		}
	case OperationStageOutlinksBatch:
		expectedKind = ChunkOutlinks
	case OperationStageDiscoveriesBatch:
		expectedKind = ChunkDiscoveries
	case OperationStageAliasesBatch:
		expectedKind = ChunkAliases
	case OperationStageImagesBatch:
		expectedKind = ChunkImages
	case OperationStageImageManifest:
		expectedKind = ChunkImageManifest
	default:
		return ErrResponseStatus
	}
	if expectedKind != "" && kind != expectedKind {
		return ErrResponseScalar
	}
	ordinal, err := parseResponseUint(tail[2])
	if err != nil {
		return err
	}
	recordCount, err := parseResponseUint(tail[3])
	if err != nil || validateOperationRecordCount(operation, recordCount) != nil {
		return ErrResponseScalar
	}
	switch operation {
	case OperationStageOutlinksBatch:
		if ordinal >= MaxOutlinkChunks {
			return ErrResponseScalar
		}
	case OperationStageDiscoveriesBatch:
		if ordinal >= MaxDiscoveryChunks {
			return ErrResponseScalar
		}
	default:
		if ordinal != 0 {
			return ErrResponseScalar
		}
	}
	dataBytes, err := parseResponseUint(tail[4])
	if err != nil || dataBytes > MaxStageAggregateLogicalDataBytes {
		return ErrResponseScalar
	}
	keyCount, err := parseResponseUint(tail[5])
	if err != nil || keyCount < 2 || keyCount > MaxStageKeys {
		return ErrResponseScalar
	}
	remaining, err := parseResponseUint(tail[6])
	if err != nil || remaining < StageControlReservationBytes || remaining > StageMemoryReservationBytes {
		return ErrResponseScalar
	}
	return nil
}

func validFinalizeReason(status Status, reason Reason) bool {
	switch status {
	case StatusCompleted:
		return reason == ReasonAllJobsTerminal
	case StatusRunBudgetExhausted:
		return reason == ReasonRequestBudgetExhausted
	case StatusRunReservationLimitExhausted:
		return reason == ReasonReservationLimitExhausted
	case StatusGroupBudgetExhausted:
		return reason == ReasonGroupBudgetsExhausted
	case StatusCancelled:
		return isCancellationReason(reason)
	case StatusNotDue:
		return reason == ReasonNone
	default:
		return false
	}
}

func responseTailFields(operation OperationName, status Status) ([]string, error) {
	switch operation {
	case OperationApproveBoot:
		return responseFieldsFor(status, []Status{StatusOK, StatusExistsIdentical}, "boot_epoch")
	case OperationMarkPlannedShutdown:
		return responseFieldsFor(status, []Status{StatusOK, StatusExistsIdentical}, "planned_nonce")
	case OperationInstallCandidateMarkers:
		return responseFieldsFor(status, []Status{StatusCandidateInstalled, StatusExistsIdentical}, "manifest_sha256", "contract_sha256")
	case OperationRetireLegacyKeys:
		return responseFieldsFor(status, []Status{StatusLegacyRetired, StatusExistsIdentical}, "deleted_bitmap", "source_sha256")
	case OperationPromoteCandidateContracts:
		return responseFieldsFor(status, []Status{StatusContractsPromoted, StatusExistsIdentical}, "manifest_sha256", "contract_sha256", "commit_guard_sha256")
	case OperationCreateRun:
		return responseFieldsFor(status, []Status{StatusCreated, StatusExistsIdentical}, "run_id")
	case OperationEnqueueBatch:
		return responseFieldsFor(status, []Status{StatusOK, StatusExistsIdentical}, "new_jobs", "reconciled_jobs", "job_count", "load_revision")
	case OperationBeginRunAudit:
		return responseFieldsFor(status, []Status{StatusAuditStarted, StatusExistsIdentical}, "audit_revision", "job_count")
	case OperationAuditRunBatch:
		return responseFieldsFor(status, []Status{StatusBatchMore, StatusBatchDone}, "checked", "audit_count", "audit_cursor_or_empty")
	case OperationSealRun:
		return responseFieldsFor(status, []Status{StatusSealed, StatusExistsIdentical}, "job_count", "source_sha256")
	case OperationActivateRun:
		return responseFieldsFor(status, []Status{StatusActivated, StatusExistsIdentical}, "activated_at_ms")
	case OperationRejectReady:
		switch status {
		case StatusDead:
			return []string{"reason"}, nil
		case StatusAuthorizationExpired, StatusRunCancelled:
			return nil, nil
		default:
			return nil, ErrResponseStatus
		}
	case OperationTryClaim:
		return claimResponseFields(status)
	case OperationRenewLease:
		switch status {
		case StatusRenewed:
			return []string{"lease_expires_at_ms"}, nil
		case StatusAuthorizationExpired, StatusRunCancelled:
			return nil, nil
		case StatusLeaseLost:
			return []string{"current_fence_or_zero"}, nil
		default:
			return nil, ErrResponseStatus
		}
	case OperationReserveRequest:
		return reserveResponseFields(status)
	case OperationStartRequest:
		switch status {
		case StatusStarted, StatusAlreadyStarted:
			return []string{"reservation_id", "started_at_ms", "delivery_attempts", "job_request_starts", "run_request_starts", "group_request_starts", "io_permission"}, nil
		case StatusRateBlocked:
			return []string{"scope_id", "next_allowed_ms", "after_io"}, nil
		case StatusAuthorizationExpired, StatusRunCancelled:
			return nil, nil
		case StatusLeaseLost:
			return []string{"current_fence_or_zero"}, nil
		default:
			return nil, ErrResponseStatus
		}
	case OperationFinishRequest:
		switch status {
		case StatusFinished, StatusAlreadyFinished:
			return []string{"reservation_id"}, nil
		case StatusLeaseLost:
			return []string{"current_fence_or_zero"}, nil
		default:
			return nil, ErrResponseStatus
		}
	case OperationCancelReservation:
		switch status {
		case StatusReservationCancelled:
			return []string{"reservation_id"}, nil
		case StatusLeaseLost:
			return []string{"current_fence_or_zero"}, nil
		default:
			return nil, ErrResponseStatus
		}
	case OperationReleaseBeforeIO:
		switch status {
		case StatusReleasedReady:
			return []string{"ready_at_ms"}, nil
		case StatusAuthorizationExpired, StatusRunCancelled:
			return nil, nil
		case StatusLeaseLost:
			return []string{"current_fence_or_zero"}, nil
		default:
			return nil, ErrResponseStatus
		}
	case OperationRetry:
		switch status {
		case StatusRetryScheduled:
			return []string{"not_before_ms", "delivery_attempts", "reason"}, nil
		case StatusDead:
			return []string{"dead_at_ms", "retry_exhausted", "last_failure_reason"}, nil
		case StatusCancelled:
			return []string{"cancelled_at_ms", "reason"}, nil
		case StatusLeaseLost:
			return []string{"current_fence_or_zero"}, nil
		default:
			return nil, ErrResponseStatus
		}
	case OperationDead:
		return leaseTerminalResponseFields(status, StatusDead)
	case OperationCancelJob:
		return leaseTerminalResponseFields(status, StatusCancelled)
	case OperationCompleteNoOutput:
		return leaseTerminalResponseFields(status, StatusCompleted)
	case OperationBeginStage:
		switch status {
		case StatusStageBegun, StatusExistsIdentical:
			return []string{"commit_id", "expires_at_ms", "memory_reservation_remaining_bytes"}, nil
		case StatusStageCapacityBlocked:
			return []string{"blocked_reason", "active_stage_slots", "maximum_stage_slots"}, nil
		case StatusAuthorizationExpired, StatusRunCancelled:
			return nil, nil
		case StatusLeaseLost:
			return []string{"current_fence_or_zero"}, nil
		default:
			return nil, ErrResponseStatus
		}
	case OperationStagePageFields, OperationStagePageBlob, OperationStageOutlinksBatch,
		OperationStageDiscoveriesBatch, OperationStageAliasesBatch, OperationStageImagesBatch,
		OperationStageImageManifest:
		switch status {
		case StatusStaged, StatusExistsIdentical:
			return []string{"commit_id", "chunk_kind", "chunk_ordinal", "accepted_records", "data_bytes", "key_count", "memory_reservation_remaining_bytes"}, nil
		case StatusAuthorizationExpired, StatusRunCancelled:
			return nil, nil
		case StatusLeaseLost:
			return []string{"current_fence_or_zero"}, nil
		default:
			return nil, ErrResponseStatus
		}
	case OperationSealStage:
		switch status {
		case StatusSealed, StatusExistsIdentical:
			return []string{"commit_id", "data_bytes", "key_count"}, nil
		case StatusAuthorizationExpired, StatusRunCancelled:
			return nil, nil
		case StatusLeaseLost:
			return []string{"current_fence_or_zero"}, nil
		default:
			return nil, ErrResponseStatus
		}
	case OperationAbortStage:
		switch status {
		case StatusStageAborted, StatusExistsIdentical:
			return []string{"commit_id", "unlinked_keys"}, nil
		case StatusLeaseLost:
			return []string{"current_fence_or_zero"}, nil
		default:
			return nil, ErrResponseStatus
		}
	case OperationCommit:
		switch status {
		case StatusCommitted, StatusAlreadyCommitted:
			return []string{"publication_id", "commit_id", "completed_at_ms"}, nil
		case StatusDownstreamBackpressure:
			return []string{"blocked_reason", "blocked_started_at_ms", "blocked_deadline_ms"}, nil
		case StatusAuthorizationExpired, StatusRunCancelled:
			return nil, nil
		case StatusLeaseLost:
			return []string{"current_fence_or_zero"}, nil
		default:
			return nil, ErrResponseStatus
		}
	case OperationPromoteDue, OperationRecoverExpired, OperationCancelBatch,
		OperationCleanStage, OperationMaintainRateScopes:
		return responseFieldsFor(status, []Status{StatusBatchMore, StatusBatchDone}, "processed", "more")
	case OperationCancelRun:
		return responseFieldsFor(status, []Status{StatusCancelled, StatusExistsIdentical}, "cancelled_at_ms", "reason")
	case OperationFinalizeRun:
		return responseFieldsFor(status, []Status{
			StatusCompleted, StatusRunBudgetExhausted, StatusRunReservationLimitExhausted,
			StatusGroupBudgetExhausted, StatusCancelled, StatusNotDue,
		}, "finalized_at_ms_or_zero", "terminal_reason")
	case OperationArchiveRun:
		switch status {
		case StatusArchived, StatusExistsIdentical:
			return []string{"archived_at_ms", "archive_sha256"}, nil
		case StatusNotDue:
			return []string{"eligible_at_ms"}, nil
		default:
			return nil, ErrResponseStatus
		}
	case OperationPurgeRunBatch:
		switch status {
		case StatusBatchMore, StatusPurged:
			return []string{"removed_jobs", "more"}, nil
		case StatusNotDue:
			return []string{"eligible_at_ms"}, nil
		default:
			return nil, ErrResponseStatus
		}
	default:
		return nil, ErrResponseStatus
	}
}

func claimResponseFields(status Status) ([]string, error) {
	switch status {
	case StatusClaimed, StatusAlreadyClaimed:
		return []string{"fence", "lease_expires_at_ms", "reservation_id", "reservation_expires_at_ms"}, nil
	case StatusVisitedCompleted:
		return []string{"completed_at_ms"}, nil
	case StatusCapacityBlocked:
		return []string{"scope_id", "active_count", "effective_concurrency", "after_io"}, nil
	case StatusRateBlocked:
		return []string{"scope_id", "next_allowed_ms", "after_io"}, nil
	case StatusRunBudgetExhausted:
		return []string{"started", "pending", "limit", "after_io"}, nil
	case StatusRunReservationLimitExhausted:
		return []string{"reservation_creations_total", "maximum_reservation_creations", "after_io"}, nil
	case StatusGroupBudgetExhausted:
		return []string{"group_id", "started", "pending", "limit", "after_io"}, nil
	case StatusLeaseCapacityBlocked:
		return []string{"active_leases", "maximum_active_leases"}, nil
	case StatusStageCapacityBlocked:
		return []string{"blocked_reason", "active_stage_slots", "maximum_stage_slots"}, nil
	case StatusNoCandidate, StatusAuthorizationExpired, StatusRunCancelled:
		return nil, nil
	case StatusLeaseLost:
		return []string{"current_fence_or_zero"}, nil
	default:
		return nil, ErrResponseStatus
	}
}

func reserveResponseFields(status Status) ([]string, error) {
	switch status {
	case StatusReserved, StatusAlreadyReserved:
		return []string{"reservation_id", "expires_at_ms"}, nil
	case StatusCapacityBlocked:
		return []string{"scope_id", "active_count", "effective_concurrency", "after_io"}, nil
	case StatusRateBlocked:
		return []string{"scope_id", "next_allowed_ms", "after_io"}, nil
	case StatusRunBudgetExhausted:
		return []string{"started", "pending", "limit", "after_io"}, nil
	case StatusRunReservationLimitExhausted:
		return []string{"reservation_creations_total", "maximum_reservation_creations", "after_io"}, nil
	case StatusGroupBudgetExhausted:
		return []string{"group_id", "started", "pending", "limit", "after_io"}, nil
	case StatusAuthorizationExpired, StatusRunCancelled:
		return nil, nil
	case StatusLeaseLost:
		return []string{"current_fence_or_zero"}, nil
	default:
		return nil, ErrResponseStatus
	}
}

func leaseTerminalResponseFields(status, requested Status) ([]string, error) {
	if status == requested || status == StatusCancelled {
		return []string{"terminal_at_ms", "reason"}, nil
	}
	if status == StatusLeaseLost {
		return []string{"current_fence_or_zero"}, nil
	}
	return nil, ErrResponseStatus
}

func responseFieldsFor(status Status, allowed []Status, fields ...string) ([]string, error) {
	for _, candidate := range allowed {
		if status == candidate {
			return append([]string(nil), fields...), nil
		}
	}
	return nil, ErrResponseStatus
}

func validateResponseField(field, value string, status Status) error {
	switch field {
	case "boot_epoch", "planned_nonce":
		if !isLowerHex(value, 32) {
			return ErrResponseScalar
		}
	case "run_id":
		if _, err := ParseRunID(value); err != nil {
			return ErrResponseScalar
		}
	case "reservation_id":
		if _, err := ParseReservationID(value); err != nil {
			return ErrResponseScalar
		}
	case "manifest_sha256", "contract_sha256", "commit_guard_sha256", "source_sha256",
		"scope_id", "commit_id", "publication_id", "archive_sha256":
		if _, err := ParseDigest(value); err != nil {
			return ErrResponseScalar
		}
	case "reason", "last_failure_reason", "terminal_reason":
		if _, err := ParseReason(value); err != nil {
			return ErrResponseScalar
		}
	case "blocked_reason":
		if _, err := ParseBlockedReason(value); err != nil {
			return ErrResponseScalar
		}
	case "chunk_kind":
		if err := validateChunkKind(ChunkKind(value)); err != nil {
			return ErrResponseScalar
		}
	case "deleted_bitmap":
		if len(value) != 5 {
			return ErrResponseScalar
		}
		for index := range value {
			if value[index] != '0' && value[index] != '1' {
				return ErrResponseScalar
			}
		}
	case "audit_cursor_or_empty":
		if value != "" {
			if _, err := ParseJobID(value); err != nil {
				return ErrResponseScalar
			}
		}
	case "group_id":
		if _, err := ParseGroupID(value); err != nil {
			return ErrResponseScalar
		}
	case "retry_exhausted":
		if value != string(ReasonRetryExhausted) {
			return ErrResponseScalar
		}
	case "after_io", "io_permission":
		if _, err := parseResponseBool(value); err != nil {
			return err
		}
	case "more":
		more, err := parseResponseBool(value)
		if err != nil {
			return err
		}
		if status == StatusBatchMore && !more || status != StatusBatchMore && more {
			return ErrResponseScalar
		}
	case "maximum_reservation_creations":
		if value != canonicalDecimal(MaxReservationCreationsPerRun) {
			return ErrResponseScalar
		}
	case "fence":
		if _, err := ParseFence(value); err != nil {
			return ErrResponseScalar
		}
	default:
		if _, err := parseResponseUint(value); err != nil {
			return err
		}
	}
	return nil
}

func parseResponseUint(value string) (uint64, error) {
	decimal, err := ParseUnsignedDecimal(value)
	if err != nil {
		return 0, ErrResponseScalar
	}
	parsed, err := strconv.ParseUint(string(decimal), 10, 64)
	if err != nil {
		return 0, ErrResponseScalar
	}
	return parsed, nil
}

func parseResponseBool(value string) (bool, error) {
	switch value {
	case "0":
		return false, nil
	case "1":
		return true, nil
	default:
		return false, ErrResponseScalar
	}
}

func responseArray(raw any) ([]any, bool) {
	switch values := raw.(type) {
	case []any:
		return values, true
	case []string:
		result := make([]any, len(values))
		for index := range values {
			result[index] = values[index]
		}
		return result, true
	default:
		return nil, false
	}
}
