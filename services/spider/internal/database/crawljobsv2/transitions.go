package crawljobsv2

import "errors"

var ErrInvalidTransitionReason = errors.New("crawljobsv2: invalid reason for transition operation")

type RejectReadyTransitionInput struct {
	RunID  RunID
	Job    SourceJob
	Reason Reason
}

type TryClaimTransitionInput struct {
	Job                SourceJob
	Lease              LeaseIdentity
	ExpectedPriorFence uint64
	InitialIntent      ReservationIntent
}

type ReleaseBeforeIOTransitionInput struct {
	Lease LeaseIdentity
}

type RetryTransitionInput struct {
	Lease  LeaseIdentity
	Reason Reason
}

type DeadTransitionInput struct {
	Lease  LeaseIdentity
	Reason Reason
}

type CancelJobTransitionInput struct {
	Lease  LeaseIdentity
	Reason Reason
}

type CompleteNoOutputTransitionInput struct {
	Lease  LeaseIdentity
	Reason Reason
}

type AbortStageTransitionInput struct {
	Lease    LeaseIdentity
	CommitID Digest
}

func DeriveRejectReadyTransitionID(input RejectReadyTransitionInput) (Digest, error) {
	if err := ValidateTransitionReason(OperationRejectReady, input.Reason); err != nil {
		return "", err
	}
	record, err := completeSourceJobRecord(input.Job)
	if err != nil {
		return "", err
	}
	payload := cloneRecord(record[1:])
	return deriveTransitionID(OperationRejectReady, input.RunID, input.Job.JobID, 0, "", input.Reason, payload)
}

func DeriveTryClaimTransitionID(input TryClaimTransitionInput) (Digest, error) {
	if err := validateLeaseIdentity(input.Lease); err != nil {
		return "", err
	}
	if input.Job.JobID != input.Lease.JobID || input.InitialIntent.Lease != input.Lease {
		return "", ErrDigestInputMismatch
	}
	if input.ExpectedPriorFence > MaxExactInteger || input.ExpectedPriorFence == MaxExactInteger || uint64(input.Lease.Fence) != input.ExpectedPriorFence+1 {
		return "", ErrInvalidFence
	}
	jobRecord, err := completeSourceJobRecord(input.Job)
	if err != nil {
		return "", err
	}
	if input.InitialIntent.Decision.RequestKind != RequestRobots && input.InitialIntent.Decision.RequestKind != RequestDocument {
		return "", ErrInvalidRequestKind
	}
	if input.InitialIntent.Decision.GroupID != input.Job.GroupID ||
		input.InitialIntent.Decision.RateScopeID != input.Job.RateScopeID ||
		input.InitialIntent.Decision.GroupScopeID != input.Job.Decision.GroupScopeID ||
		input.InitialIntent.Decision.OriginScopeID != input.Job.Decision.OriginScopeID ||
		input.InitialIntent.Decision.Depth != input.Job.Depth ||
		input.InitialIntent.Decision.GroupConcurrency != input.Job.Decision.GroupConcurrency ||
		input.InitialIntent.Decision.GroupIntervalMS != input.Job.Decision.GroupIntervalMS ||
		input.InitialIntent.Decision.OriginConcurrency != input.Job.Decision.OriginConcurrency ||
		input.InitialIntent.Decision.OriginIntervalMS != input.Job.Decision.OriginIntervalMS {
		return "", ErrDigestInputMismatch
	}
	intentFields, err := reservationIntentFields(input.InitialIntent)
	if err != nil {
		return "", err
	}

	payload := Record{
		cloneField(jobRecord[1]),
		cloneField(jobRecord[2]),
		cloneField(jobRecord[3]),
		textField("job_group_id", string(input.Job.GroupID)),
		textField("job_rate_scope_id", string(input.Job.RateScopeID)),
		textField("job_group_scope_id", string(input.Job.Decision.GroupScopeID)),
		textField("job_initial_origin_scope_id", string(input.Job.Decision.OriginScopeID)),
		textField("job_policy_decision_sha256", string(jobRecord[8].Value)),
		textField("expected_prior_fence", canonicalDecimal(input.ExpectedPriorFence)),
		textField("owner_id", string(input.Lease.OwnerID)),
	}
	payload = append(payload, intentFields...)
	return deriveTransitionID(
		OperationTryClaim,
		input.Lease.RunID,
		input.Lease.JobID,
		input.Lease.Fence,
		input.Lease.Token,
		ReasonNone,
		payload,
	)
}

func DeriveReleaseBeforeIOTransitionID(input ReleaseBeforeIOTransitionInput) (Digest, error) {
	return deriveLeaseTransitionID(OperationReleaseBeforeIO, input.Lease, ReasonNone, nil)
}

func DeriveRetryTransitionID(input RetryTransitionInput) (Digest, error) {
	return deriveLeaseTransitionID(OperationRetry, input.Lease, input.Reason, nil)
}

func DeriveDeadTransitionID(input DeadTransitionInput) (Digest, error) {
	return deriveLeaseTransitionID(OperationDead, input.Lease, input.Reason, nil)
}

func DeriveCancelJobTransitionID(input CancelJobTransitionInput) (Digest, error) {
	return deriveLeaseTransitionID(OperationCancelJob, input.Lease, input.Reason, nil)
}

func DeriveCompleteNoOutputTransitionID(input CompleteNoOutputTransitionInput) (Digest, error) {
	return deriveLeaseTransitionID(OperationCompleteNoOutput, input.Lease, input.Reason, nil)
}

func DeriveAbortStageTransitionID(input AbortStageTransitionInput) (Digest, error) {
	if err := validateDigest(input.CommitID); err != nil {
		return "", err
	}
	return deriveLeaseTransitionID(
		OperationAbortStage,
		input.Lease,
		ReasonNone,
		Record{textField("commit_id", string(input.CommitID))},
	)
}

func ValidateTransitionReason(operation OperationName, reason Reason) error {
	if _, err := ParseReason(string(reason)); err != nil {
		return err
	}
	valid := false
	switch operation {
	case OperationRejectReady:
		valid = isOneOfReason(reason,
			ReasonPolicyDenied,
			ReasonPolicyScopeChanged,
			ReasonJobMalformed,
			ReasonURLIdentityMismatch,
			ReasonStaticURLDenied,
		)
	case OperationTryClaim, OperationReleaseBeforeIO, OperationAbortStage:
		valid = reason == ReasonNone
	case OperationRetry:
		valid = isRetryableReason(reason) && reason != ReasonLeaseExpiredAfterIO
	case OperationDead:
		valid = isDeadLetterReason(reason) &&
			reason != ReasonPolicyScopeChanged &&
			reason != ReasonRetryExhausted &&
			reason != ReasonPreIORecoveryExhausted
	case OperationCancelJob:
		valid = isCancellationReason(reason)
	case OperationCompleteNoOutput:
		valid = reason == ReasonAlreadyVisited
	default:
		return ErrUnknownOperation
	}
	if !valid {
		return ErrInvalidTransitionReason
	}
	return nil
}

func deriveLeaseTransitionID(operation OperationName, lease LeaseIdentity, reason Reason, suffix Record) (Digest, error) {
	if err := validateLeaseIdentity(lease); err != nil {
		return "", err
	}
	if err := ValidateTransitionReason(operation, reason); err != nil {
		return "", err
	}
	payload := Record{textField("owner_id", string(lease.OwnerID))}
	payload = append(payload, cloneRecord(suffix)...)
	return deriveTransitionID(operation, lease.RunID, lease.JobID, lease.Fence, lease.Token, reason, payload)
}

func reservationIntentFields(intent ReservationIntent) (Record, error) {
	if _, err := DeriveReservationID(intent); err != nil {
		return nil, err
	}
	decisionDigest, err := DerivePolicyDecisionDigest(intent.Decision)
	if err != nil {
		return nil, err
	}
	return Record{
		textField("request_ordinal", canonicalDecimal(intent.RequestOrdinal)),
		textField("request_kind", string(intent.Decision.RequestKind)),
		textField("target_url_id", string(intent.Target.URLID)),
		textField("canonical_target_url", intent.Target.CanonicalURL),
		textField("target_digest", string(intent.Decision.TargetDigest)),
		textField("crawl_policy_sha256", string(intent.CrawlPolicyDigest)),
		textField("policy_decision_sha256", string(decisionDigest)),
		textField("group_id", string(intent.Decision.GroupID)),
		textField("rate_scope_id", string(intent.Decision.RateScopeID)),
		textField("global_scope_id", string(intent.Decision.GlobalScopeID)),
		textField("group_scope_id", string(intent.Decision.GroupScopeID)),
		textField("origin_scope_id", string(intent.Decision.OriginScopeID)),
		textField("global_concurrency", canonicalDecimal(intent.Decision.GlobalConcurrency)),
		textField("global_interval_ms", canonicalDecimal(intent.Decision.GlobalIntervalMS)),
		textField("group_concurrency", canonicalDecimal(intent.Decision.GroupConcurrency)),
		textField("group_interval_ms", canonicalDecimal(intent.Decision.GroupIntervalMS)),
		textField("origin_concurrency", canonicalDecimal(intent.Decision.OriginConcurrency)),
		textField("origin_interval_ms", canonicalDecimal(intent.Decision.OriginIntervalMS)),
	}, nil
}

func isRetryableReason(reason Reason) bool {
	return isOneOfReason(reason,
		ReasonRequestTimeout,
		ReasonDNSTemporary,
		ReasonDialTemporary,
		ReasonRequestTemporary,
		ReasonHTTP429,
		ReasonHTTP5xx,
		ReasonRobotsTemporary,
		ReasonRendererTemporary,
		ReasonDownstreamBackpressure,
		ReasonCapacityBlockedAfterIO,
		ReasonRunBudgetExhaustedAfterIO,
		ReasonGroupBudgetExhaustedAfterIO,
		ReasonRateBlockedAfterIO,
		ReasonLeaseExpiredAfterIO,
		ReasonWorkerShutdownAfterIO,
	)
}

func isDeadLetterReason(reason Reason) bool {
	return isOneOfReason(reason,
		ReasonPolicyDenied,
		ReasonPolicyScopeChanged,
		ReasonRobotsDenied,
		ReasonRobotsInvalid,
		ReasonJobMalformed,
		ReasonURLIdentityMismatch,
		ReasonStaticURLDenied,
		ReasonDNSProhibited,
		ReasonHTTP4xx,
		ReasonResponseInvalid,
		ReasonBodyTooLarge,
		ReasonHTMLInvalid,
		ReasonDiscoveryLimit,
		ReasonRendererPermanent,
		ReasonOutputInvalid,
		ReasonRunJobLimit,
		ReasonReservationLimitExhausted,
		ReasonRetryExhausted,
		ReasonPreIORecoveryExhausted,
		ReasonProtocolCorrupt,
	)
}

func isCancellationReason(reason Reason) bool {
	return isOneOfReason(reason, ReasonAuthorizationExpired, ReasonOperatorCancelled, ReasonSourceCancelled)
}

func isOneOfReason(reason Reason, allowed ...Reason) bool {
	for _, candidate := range allowed {
		if reason == candidate {
			return true
		}
	}
	return false
}

func cloneField(field Field) Field {
	return Field{Name: field.Name, Value: append([]byte(nil), field.Value...)}
}

func cloneRecord(record Record) Record {
	cloned := make(Record, len(record))
	for index := range record {
		cloned[index] = cloneField(record[index])
	}
	return cloned
}
