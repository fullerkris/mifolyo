package crawljobsv2

import "errors"

type Status string

const (
	StatusOK                           Status = "OK"
	StatusCreated                      Status = "CREATED"
	StatusExistsIdentical              Status = "EXISTS_IDENTICAL"
	StatusCandidateInstalled           Status = "CANDIDATE_INSTALLED"
	StatusLegacyRetired                Status = "LEGACY_RETIRED"
	StatusContractsPromoted            Status = "CONTRACTS_PROMOTED"
	StatusSealed                       Status = "SEALED"
	StatusActivated                    Status = "ACTIVATED"
	StatusAuditStarted                 Status = "AUDIT_STARTED"
	StatusClaimed                      Status = "CLAIMED"
	StatusAlreadyClaimed               Status = "ALREADY_CLAIMED"
	StatusNoCandidate                  Status = "NO_CANDIDATE"
	StatusVisitedCompleted             Status = "VISITED_COMPLETED"
	StatusReserved                     Status = "RESERVED"
	StatusAlreadyReserved              Status = "ALREADY_RESERVED"
	StatusStarted                      Status = "STARTED"
	StatusAlreadyStarted               Status = "ALREADY_STARTED"
	StatusFinished                     Status = "FINISHED"
	StatusAlreadyFinished              Status = "ALREADY_FINISHED"
	StatusReservationCancelled         Status = "RESERVATION_CANCELLED"
	StatusReleasedReady                Status = "RELEASED_READY"
	StatusRenewed                      Status = "RENEWED"
	StatusRetryScheduled               Status = "RETRY_SCHEDULED"
	StatusStageBegun                   Status = "STAGE_BEGUN"
	StatusStaged                       Status = "STAGED"
	StatusStageAborted                 Status = "STAGE_ABORTED"
	StatusCompleted                    Status = "COMPLETED"
	StatusDead                         Status = "DEAD"
	StatusCancelled                    Status = "CANCELLED"
	StatusCommitted                    Status = "COMMITTED"
	StatusAlreadyCommitted             Status = "ALREADY_COMMITTED"
	StatusCapacityBlocked              Status = "CAPACITY_BLOCKED"
	StatusRateBlocked                  Status = "RATE_BLOCKED"
	StatusLeaseCapacityBlocked         Status = "LEASE_CAPACITY_BLOCKED"
	StatusStageCapacityBlocked         Status = "STAGE_CAPACITY_BLOCKED"
	StatusRunBudgetExhausted           Status = "RUN_BUDGET_EXHAUSTED"
	StatusRunReservationLimitExhausted Status = "RUN_RESERVATION_LIMIT_EXHAUSTED"
	StatusGroupBudgetExhausted         Status = "GROUP_BUDGET_EXHAUSTED"
	StatusDownstreamBackpressure       Status = "DOWNSTREAM_BACKPRESSURE"
	StatusAuthorizationExpired         Status = "AUTHORIZATION_EXPIRED"
	StatusRunCancelled                 Status = "RUN_CANCELLED"
	StatusLeaseLost                    Status = "LEASE_LOST"
	StatusNotDue                       Status = "NOT_DUE"
	StatusBatchMore                    Status = "BATCH_MORE"
	StatusBatchDone                    Status = "BATCH_DONE"
	StatusArchived                     Status = "ARCHIVED"
	StatusPurged                       Status = "PURGED"
)

var ErrUnknownStatus = errors.New("crawljobsv2: unknown response status")

var statuses = map[Status]struct{}{
	StatusOK: {}, StatusCreated: {}, StatusExistsIdentical: {},
	StatusCandidateInstalled: {}, StatusLegacyRetired: {}, StatusContractsPromoted: {},
	StatusSealed: {}, StatusActivated: {}, StatusAuditStarted: {}, StatusClaimed: {},
	StatusAlreadyClaimed: {}, StatusNoCandidate: {}, StatusVisitedCompleted: {},
	StatusReserved: {}, StatusAlreadyReserved: {}, StatusStarted: {}, StatusAlreadyStarted: {},
	StatusFinished: {}, StatusAlreadyFinished: {}, StatusReservationCancelled: {},
	StatusReleasedReady: {}, StatusRenewed: {}, StatusRetryScheduled: {}, StatusStageBegun: {},
	StatusStaged: {}, StatusStageAborted: {}, StatusCompleted: {}, StatusDead: {},
	StatusCancelled: {}, StatusCommitted: {}, StatusAlreadyCommitted: {},
	StatusCapacityBlocked: {}, StatusRateBlocked: {}, StatusLeaseCapacityBlocked: {},
	StatusStageCapacityBlocked: {}, StatusRunBudgetExhausted: {},
	StatusRunReservationLimitExhausted: {}, StatusGroupBudgetExhausted: {},
	StatusDownstreamBackpressure: {}, StatusAuthorizationExpired: {}, StatusRunCancelled: {},
	StatusLeaseLost: {}, StatusNotDue: {}, StatusBatchMore: {}, StatusBatchDone: {},
	StatusArchived: {}, StatusPurged: {},
}

func ParseStatus(value string) (Status, error) {
	status := Status(value)
	if _, ok := statuses[status]; !ok {
		return "", ErrUnknownStatus
	}
	return status, nil
}

type ErrorCode string

const (
	ErrorBootUnapproved            ErrorCode = "BOOT_UNAPPROVED"
	ErrorCompatibilityMismatch     ErrorCode = "COMPATIBILITY_MISMATCH"
	ErrorContractMismatch          ErrorCode = "CONTRACT_MISMATCH"
	ErrorWrongType                 ErrorCode = "WRONG_TYPE"
	ErrorInvalidArgument           ErrorCode = "INVALID_ARGUMENT"
	ErrorInvalidIdentifier         ErrorCode = "INVALID_IDENTIFIER"
	ErrorInvalidNumber             ErrorCode = "INVALID_NUMBER"
	ErrorInvalidState              ErrorCode = "INVALID_STATE"
	ErrorImmutableMismatch         ErrorCode = "IMMUTABLE_MISMATCH"
	ErrorURLIDCollision            ErrorCode = "URL_ID_COLLISION"
	ErrorLimitExceeded             ErrorCode = "LIMIT_EXCEEDED"
	ErrorCounterCorrupt            ErrorCode = "COUNTER_CORRUPT"
	ErrorStateIndexCorrupt         ErrorCode = "STATE_INDEX_CORRUPT"
	ErrorReservationCorrupt        ErrorCode = "RESERVATION_CORRUPT"
	ErrorRateStateCorrupt          ErrorCode = "RATE_STATE_CORRUPT"
	ErrorStageInvalid              ErrorCode = "STAGE_INVALID"
	ErrorStageUnsealed             ErrorCode = "STAGE_UNSEALED"
	ErrorDestinationExists         ErrorCode = "DESTINATION_EXISTS"
	ErrorOutputContractMismatch    ErrorCode = "OUTPUT_CONTRACT_MISMATCH"
	ErrorCommandBoundsExceeded     ErrorCode = "COMMAND_BOUNDS_EXCEEDED"
	ErrorMemoryHeadroomLow         ErrorCode = "MEMORY_HEADROOM_LOW"
	ErrorRateScopeCapacityExceeded ErrorCode = "RATE_SCOPE_CAPACITY_EXCEEDED"
	ErrorAdminFreezeRequired       ErrorCode = "ADMIN_FREEZE_REQUIRED"
	ErrorCommitGuardUnapproved     ErrorCode = "COMMIT_GUARD_UNAPPROVED"
)

var ErrUnknownErrorCode = errors.New("crawljobsv2: unknown Redis error code")

var errorCodes = map[ErrorCode]struct{}{
	ErrorBootUnapproved: {}, ErrorCompatibilityMismatch: {}, ErrorContractMismatch: {},
	ErrorWrongType: {}, ErrorInvalidArgument: {}, ErrorInvalidIdentifier: {},
	ErrorInvalidNumber: {}, ErrorInvalidState: {}, ErrorImmutableMismatch: {},
	ErrorURLIDCollision: {}, ErrorLimitExceeded: {}, ErrorCounterCorrupt: {},
	ErrorStateIndexCorrupt: {}, ErrorReservationCorrupt: {}, ErrorRateStateCorrupt: {},
	ErrorStageInvalid: {}, ErrorStageUnsealed: {}, ErrorDestinationExists: {},
	ErrorOutputContractMismatch: {}, ErrorCommandBoundsExceeded: {}, ErrorMemoryHeadroomLow: {},
	ErrorRateScopeCapacityExceeded: {}, ErrorAdminFreezeRequired: {}, ErrorCommitGuardUnapproved: {},
}

func ParseErrorCode(value string) (ErrorCode, error) {
	code := ErrorCode(value)
	if _, ok := errorCodes[code]; !ok {
		return "", ErrUnknownErrorCode
	}
	return code, nil
}

func (code ErrorCode) RedisReply() (string, error) {
	if _, ok := errorCodes[code]; !ok {
		return "", ErrUnknownErrorCode
	}
	return "ERR CRAWL_V2_" + string(code), nil
}

type Reason string

const (
	ReasonNone                        Reason = "none"
	ReasonPublished                   Reason = "published"
	ReasonAlreadyVisited              Reason = "already_visited"
	ReasonRequestTimeout              Reason = "request_timeout"
	ReasonDNSTemporary                Reason = "dns_temporary"
	ReasonDialTemporary               Reason = "dial_temporary"
	ReasonRequestTemporary            Reason = "request_temporary"
	ReasonHTTP429                     Reason = "http_429"
	ReasonHTTP5xx                     Reason = "http_5xx"
	ReasonRobotsTemporary             Reason = "robots_temporary"
	ReasonRendererTemporary           Reason = "renderer_temporary"
	ReasonDownstreamBackpressure      Reason = "downstream_backpressure"
	ReasonCapacityBlockedAfterIO      Reason = "capacity_blocked_after_io"
	ReasonRunBudgetExhaustedAfterIO   Reason = "run_budget_exhausted_after_io"
	ReasonGroupBudgetExhaustedAfterIO Reason = "group_budget_exhausted_after_io"
	ReasonRateBlockedAfterIO          Reason = "rate_blocked_after_io"
	ReasonLeaseExpiredAfterIO         Reason = "lease_expired_after_io"
	ReasonWorkerShutdownAfterIO       Reason = "worker_shutdown_after_io"
	ReasonPolicyDenied                Reason = "policy_denied"
	ReasonPolicyScopeChanged          Reason = "policy_scope_changed"
	ReasonRobotsDenied                Reason = "robots_denied"
	ReasonRobotsInvalid               Reason = "robots_invalid"
	ReasonJobMalformed                Reason = "job_malformed"
	ReasonURLIdentityMismatch         Reason = "url_identity_mismatch"
	ReasonStaticURLDenied             Reason = "static_url_denied"
	ReasonDNSProhibited               Reason = "dns_prohibited"
	ReasonHTTP4xx                     Reason = "http_4xx"
	ReasonResponseInvalid             Reason = "response_invalid"
	ReasonBodyTooLarge                Reason = "body_too_large"
	ReasonHTMLInvalid                 Reason = "html_invalid"
	ReasonDiscoveryLimit              Reason = "discovery_limit"
	ReasonRendererPermanent           Reason = "renderer_permanent"
	ReasonOutputInvalid               Reason = "output_invalid"
	ReasonRunJobLimit                 Reason = "run_job_limit"
	ReasonReservationLimitExhausted   Reason = "reservation_limit_exhausted"
	ReasonRetryExhausted              Reason = "retry_exhausted"
	ReasonPreIORecoveryExhausted      Reason = "pre_io_recovery_exhausted"
	ReasonProtocolCorrupt             Reason = "protocol_corrupt"
	ReasonAuthorizationExpired        Reason = "authorization_expired"
	ReasonOperatorCancelled           Reason = "operator_cancelled"
	ReasonSourceCancelled             Reason = "source_cancelled"
	ReasonAllJobsTerminal             Reason = "all_jobs_terminal"
	ReasonRequestBudgetExhausted      Reason = "request_budget_exhausted"
	ReasonGroupBudgetsExhausted       Reason = "group_budgets_exhausted"
)

var ErrUnknownReason = errors.New("crawljobsv2: unknown reason")

var reasons = map[Reason]struct{}{
	ReasonNone: {}, ReasonPublished: {}, ReasonAlreadyVisited: {}, ReasonRequestTimeout: {},
	ReasonDNSTemporary: {}, ReasonDialTemporary: {}, ReasonRequestTemporary: {}, ReasonHTTP429: {},
	ReasonHTTP5xx: {}, ReasonRobotsTemporary: {}, ReasonRendererTemporary: {},
	ReasonDownstreamBackpressure: {}, ReasonCapacityBlockedAfterIO: {},
	ReasonRunBudgetExhaustedAfterIO: {}, ReasonGroupBudgetExhaustedAfterIO: {},
	ReasonRateBlockedAfterIO: {}, ReasonLeaseExpiredAfterIO: {}, ReasonWorkerShutdownAfterIO: {},
	ReasonPolicyDenied: {}, ReasonPolicyScopeChanged: {}, ReasonRobotsDenied: {},
	ReasonRobotsInvalid: {}, ReasonJobMalformed: {}, ReasonURLIdentityMismatch: {},
	ReasonStaticURLDenied: {}, ReasonDNSProhibited: {}, ReasonHTTP4xx: {},
	ReasonResponseInvalid: {}, ReasonBodyTooLarge: {}, ReasonHTMLInvalid: {},
	ReasonDiscoveryLimit: {}, ReasonRendererPermanent: {}, ReasonOutputInvalid: {},
	ReasonRunJobLimit: {}, ReasonReservationLimitExhausted: {}, ReasonRetryExhausted: {},
	ReasonPreIORecoveryExhausted: {}, ReasonProtocolCorrupt: {}, ReasonAuthorizationExpired: {},
	ReasonOperatorCancelled: {}, ReasonSourceCancelled: {}, ReasonAllJobsTerminal: {},
	ReasonRequestBudgetExhausted: {}, ReasonGroupBudgetsExhausted: {},
}

func ParseReason(value string) (Reason, error) {
	reason := Reason(value)
	if _, ok := reasons[reason]; !ok {
		return "", ErrUnknownReason
	}
	return reason, nil
}

type BlockedReason string

const (
	BlockedPagesQueueFull    BlockedReason = "pages_queue_full"
	BlockedMemoryHeadroomLow BlockedReason = "memory_headroom_low"
	BlockedStageSlotsFull    BlockedReason = "stage_slots_full"
)

var ErrUnknownBlockedReason = errors.New("crawljobsv2: unknown blocked reason")

func ParseBlockedReason(value string) (BlockedReason, error) {
	reason := BlockedReason(value)
	switch reason {
	case BlockedPagesQueueFull, BlockedMemoryHeadroomLow, BlockedStageSlotsFull:
		return reason, nil
	default:
		return "", ErrUnknownBlockedReason
	}
}

type OperationName string

const (
	OperationApproveBoot               OperationName = "CJ2_APPROVE_BOOT"
	OperationInstallCandidateMarkers   OperationName = "CJ2_INSTALL_CANDIDATE_MARKERS"
	OperationRetireLegacyKeys          OperationName = "CJ2_RETIRE_LEGACY_KEYS"
	OperationPromoteCandidateContracts OperationName = "CJ2_PROMOTE_CANDIDATE_CONTRACTS"
	OperationMarkPlannedShutdown       OperationName = "CJ2_MARK_PLANNED_SHUTDOWN"
	OperationCreateRun                 OperationName = "CJ2_CREATE_RUN"
	OperationEnqueueBatch              OperationName = "CJ2_ENQUEUE_BATCH"
	OperationBeginRunAudit             OperationName = "CJ2_BEGIN_RUN_AUDIT"
	OperationAuditRunBatch             OperationName = "CJ2_AUDIT_RUN_BATCH"
	OperationSealRun                   OperationName = "CJ2_SEAL_RUN"
	OperationActivateRun               OperationName = "CJ2_ACTIVATE_RUN"
	OperationRejectReady               OperationName = "CJ2_REJECT_READY"
	OperationTryClaim                  OperationName = "CJ2_TRY_CLAIM"
	OperationRenewLease                OperationName = "CJ2_RENEW_LEASE"
	OperationReserveRequest            OperationName = "CJ2_RESERVE_REQUEST"
	OperationStartRequest              OperationName = "CJ2_START_REQUEST"
	OperationFinishRequest             OperationName = "CJ2_FINISH_REQUEST"
	OperationCancelReservation         OperationName = "CJ2_CANCEL_RESERVATION"
	OperationReleaseBeforeIO           OperationName = "CJ2_RELEASE_BEFORE_IO"
	OperationRetry                     OperationName = "CJ2_RETRY"
	OperationDead                      OperationName = "CJ2_DEAD"
	OperationCancelJob                 OperationName = "CJ2_CANCEL_JOB"
	OperationCompleteNoOutput          OperationName = "CJ2_COMPLETE_NO_OUTPUT"
	OperationBeginStage                OperationName = "CJ2_BEGIN_STAGE"
	OperationStagePageFields           OperationName = "CJ2_STAGE_PAGE_FIELDS"
	OperationStagePageBlob             OperationName = "CJ2_STAGE_PAGE_BLOB"
	OperationStageOutlinksBatch        OperationName = "CJ2_STAGE_OUTLINKS_BATCH"
	OperationStageDiscoveriesBatch     OperationName = "CJ2_STAGE_DISCOVERIES_BATCH"
	OperationStageAliasesBatch         OperationName = "CJ2_STAGE_ALIASES_BATCH"
	OperationStageImagesBatch          OperationName = "CJ2_STAGE_IMAGES_BATCH"
	OperationStageImageManifest        OperationName = "CJ2_STAGE_IMAGE_MANIFEST"
	OperationAbortStage                OperationName = "CJ2_ABORT_STAGE"
	OperationSealStage                 OperationName = "CJ2_SEAL_STAGE"
	OperationCommit                    OperationName = "CJ2_COMMIT"
	OperationPromoteDue                OperationName = "CJ2_PROMOTE_DUE"
	OperationRecoverExpired            OperationName = "CJ2_RECOVER_EXPIRED"
	OperationCancelRun                 OperationName = "CJ2_CANCEL_RUN"
	OperationCancelBatch               OperationName = "CJ2_CANCEL_BATCH"
	OperationFinalizeRun               OperationName = "CJ2_FINALIZE_RUN"
	OperationArchiveRun                OperationName = "CJ2_ARCHIVE_RUN"
	OperationPurgeRunBatch             OperationName = "CJ2_PURGE_RUN_BATCH"
	OperationCleanStage                OperationName = "CJ2_CLEAN_STAGE"
	OperationMaintainRateScopes        OperationName = "CJ2_MAINTAIN_RATE_SCOPES"
)

var ErrUnknownOperation = errors.New("crawljobsv2: unknown operation")

var operations = map[OperationName]struct{}{
	OperationApproveBoot: {}, OperationInstallCandidateMarkers: {}, OperationRetireLegacyKeys: {},
	OperationPromoteCandidateContracts: {}, OperationMarkPlannedShutdown: {}, OperationCreateRun: {},
	OperationEnqueueBatch: {}, OperationBeginRunAudit: {}, OperationAuditRunBatch: {}, OperationSealRun: {},
	OperationActivateRun: {}, OperationRejectReady: {}, OperationTryClaim: {}, OperationRenewLease: {},
	OperationReserveRequest: {}, OperationStartRequest: {}, OperationFinishRequest: {},
	OperationCancelReservation: {}, OperationReleaseBeforeIO: {}, OperationRetry: {}, OperationDead: {},
	OperationCancelJob: {}, OperationCompleteNoOutput: {}, OperationBeginStage: {}, OperationStagePageFields: {},
	OperationStagePageBlob: {}, OperationStageOutlinksBatch: {}, OperationStageDiscoveriesBatch: {},
	OperationStageAliasesBatch: {}, OperationStageImagesBatch: {}, OperationStageImageManifest: {},
	OperationAbortStage: {}, OperationSealStage: {}, OperationCommit: {}, OperationPromoteDue: {},
	OperationRecoverExpired: {}, OperationCancelRun: {}, OperationCancelBatch: {}, OperationFinalizeRun: {},
	OperationArchiveRun: {}, OperationPurgeRunBatch: {}, OperationCleanStage: {}, OperationMaintainRateScopes: {},
}

func ParseOperationName(value string) (OperationName, error) {
	operation := OperationName(value)
	if _, ok := operations[operation]; !ok {
		return "", ErrUnknownOperation
	}
	return operation, nil
}

type ChunkKind string

const (
	ChunkPageFields    ChunkKind = "page_fields"
	ChunkHTML          ChunkKind = "html"
	ChunkOriginalHTML  ChunkKind = "original_html"
	ChunkOutlinks      ChunkKind = "outlinks"
	ChunkDiscoveries   ChunkKind = "discoveries"
	ChunkAliases       ChunkKind = "aliases"
	ChunkImages        ChunkKind = "images"
	ChunkImageManifest ChunkKind = "image_manifest"
)

var ErrUnknownChunkKind = errors.New("crawljobsv2: unknown chunk kind")

func validateChunkKind(kind ChunkKind) error {
	switch kind {
	case ChunkPageFields, ChunkHTML, ChunkOriginalHTML, ChunkOutlinks, ChunkDiscoveries, ChunkAliases, ChunkImages, ChunkImageManifest:
		return nil
	default:
		return ErrUnknownChunkKind
	}
}
