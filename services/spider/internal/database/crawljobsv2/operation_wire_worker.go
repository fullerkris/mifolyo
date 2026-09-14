package crawljobsv2

func NewRejectReadyWireRequest(gate TransportGate, runPolicy RunPolicyAuthority, input RejectReadyTransitionInput) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationRejectReady, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if validateRunID(input.RunID) != nil {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	binding, err := validateSourceJobsAgainstRunPolicy(runPolicy, []SourceJob{input.Job})
	if err != nil {
		return OperationWireRequest{}, err
	}
	if binding.runID != input.RunID {
		return OperationWireRequest{}, ErrPolicyGroupBindingMismatch
	}
	record, err := completeSourceJobRecord(input.Job)
	if err != nil {
		return OperationWireRequest{}, err
	}
	transitionID, err := DeriveRejectReadyTransitionID(runPolicy, input)
	if err != nil {
		return OperationWireRequest{}, err
	}
	semantic := operationWireFields(textField("run_id", string(input.RunID)))
	semantic = append(semantic, cloneRecord(record)...)
	semantic = append(semantic,
		textField("reason", string(input.Reason)),
		textField("transition_id", string(transitionID)),
	)
	return newOperationWireRequest(
		OperationRejectReady, gatePointer, semantic, nil, nil,
		operationWireKeyContext{runID: input.RunID, jobID: input.Job.JobID}, operationWireChunkContext{},
	)
}

func NewTryClaimWireRequest(gate TransportGate, runPolicy RunPolicyAuthority, input TryClaimTransitionInput) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationTryClaim, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if _, err := validateSourceJobsAgainstRunPolicy(runPolicy, []SourceJob{input.Job}); err != nil {
		return OperationWireRequest{}, err
	}
	if _, err := validateReservationIntentAgainstRunPolicy(runPolicy, input.InitialIntent); err != nil {
		return OperationWireRequest{}, err
	}
	jobRecord, err := completeSourceJobRecord(input.Job)
	if err != nil {
		return OperationWireRequest{}, err
	}
	intentFields, err := reservationIntentFields(runPolicy, input.InitialIntent)
	if err != nil {
		return OperationWireRequest{}, err
	}
	reservationID, err := DeriveReservationID(runPolicy, input.InitialIntent)
	if err != nil {
		return OperationWireRequest{}, err
	}
	transitionID, err := DeriveTryClaimTransitionID(runPolicy, input)
	if err != nil {
		return OperationWireRequest{}, err
	}
	semantic := operationWireFields(
		textField("run_id", string(input.Lease.RunID)),
		cloneField(jobRecord[0]),
		cloneField(jobRecord[1]),
		cloneField(jobRecord[2]),
		cloneField(jobRecord[3]),
		textField("job_group_id", string(jobRecord[4].Value)),
		textField("job_rate_scope_id", string(jobRecord[5].Value)),
		textField("job_group_scope_id", string(jobRecord[6].Value)),
		textField("job_initial_origin_scope_id", string(jobRecord[7].Value)),
		textField("job_policy_decision_sha256", string(jobRecord[8].Value)),
		textField("expected_prior_fence", canonicalDecimal(input.ExpectedPriorFence)),
		textField("fence", canonicalDecimal(uint64(input.Lease.Fence))),
		textField("owner_id", string(input.Lease.OwnerID)),
		textField("lease_token", string(input.Lease.Token)),
	)
	semantic = append(semantic, cloneRecord(intentFields)...)
	semantic = append(semantic, textField("transition_id", string(transitionID)))
	return newOperationWireRequest(
		OperationTryClaim,
		gatePointer,
		semantic,
		nil,
		nil,
		operationWireReservationContext(input.InitialIntent, reservationID),
		operationWireChunkContext{},
	)
}

func NewRenewLeaseWireRequest(gate TransportGate, lease LeaseIdentity) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationRenewLease, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	semantic, err := operationWireLeaseFields(lease)
	if err != nil {
		return OperationWireRequest{}, err
	}
	return newOperationWireRequest(
		OperationRenewLease, gatePointer, semantic, nil, nil,
		operationWireKeyContext{runID: lease.RunID, jobID: lease.JobID}, operationWireChunkContext{},
	)
}

func NewReserveRequestWireRequest(gate TransportGate, runPolicy RunPolicyAuthority, intent ReservationIntent) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationReserveRequest, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if _, err := validateReservationIntentAgainstRunPolicy(runPolicy, intent); err != nil {
		return OperationWireRequest{}, err
	}
	leaseFields, err := operationWireLeaseFields(intent.Lease)
	if err != nil {
		return OperationWireRequest{}, err
	}
	intentFields, err := reservationIntentFields(runPolicy, intent)
	if err != nil {
		return OperationWireRequest{}, err
	}
	reservationID, err := DeriveReservationID(runPolicy, intent)
	if err != nil {
		return OperationWireRequest{}, err
	}
	semantic := append(leaseFields, cloneRecord(intentFields)...)
	return newOperationWireRequest(
		OperationReserveRequest, gatePointer, semantic, nil, nil,
		operationWireReservationContext(intent, reservationID), operationWireChunkContext{},
	)
}

func NewStartRequestWireRequest(gate TransportGate, runPolicy RunPolicyAuthority, intent ReservationIntent) (OperationWireRequest, error) {
	return newReservationIdentityWireRequest(OperationStartRequest, gate, runPolicy, intent)
}

func NewFinishRequestWireRequest(gate TransportGate, runPolicy RunPolicyAuthority, intent ReservationIntent) (OperationWireRequest, error) {
	return newReservationIdentityWireRequest(OperationFinishRequest, gate, runPolicy, intent)
}

func NewCancelReservationWireRequest(gate TransportGate, runPolicy RunPolicyAuthority, intent ReservationIntent) (OperationWireRequest, error) {
	return newReservationIdentityWireRequest(OperationCancelReservation, gate, runPolicy, intent)
}

func NewReleaseBeforeIOWireRequest(gate TransportGate, input ReleaseBeforeIOTransitionInput) (OperationWireRequest, error) {
	transitionID, err := DeriveReleaseBeforeIOTransitionID(input)
	if err != nil {
		return OperationWireRequest{}, err
	}
	return newLeaseTransitionWireRequest(OperationReleaseBeforeIO, gate, input.Lease, ReasonNone, transitionID)
}

func NewRetryWireRequest(gate TransportGate, input RetryTransitionInput) (OperationWireRequest, error) {
	transitionID, err := DeriveRetryTransitionID(input)
	if err != nil {
		return OperationWireRequest{}, err
	}
	return newLeaseTransitionWireRequest(OperationRetry, gate, input.Lease, input.Reason, transitionID)
}

func NewDeadWireRequest(gate TransportGate, input DeadTransitionInput) (OperationWireRequest, error) {
	transitionID, err := DeriveDeadTransitionID(input)
	if err != nil {
		return OperationWireRequest{}, err
	}
	return newLeaseTransitionWireRequest(OperationDead, gate, input.Lease, input.Reason, transitionID)
}

func NewCancelJobWireRequest(gate TransportGate, input CancelJobTransitionInput) (OperationWireRequest, error) {
	transitionID, err := DeriveCancelJobTransitionID(input)
	if err != nil {
		return OperationWireRequest{}, err
	}
	return newLeaseTransitionWireRequest(OperationCancelJob, gate, input.Lease, input.Reason, transitionID)
}

func NewCompleteNoOutputWireRequest(gate TransportGate, input CompleteNoOutputTransitionInput) (OperationWireRequest, error) {
	transitionID, err := DeriveCompleteNoOutputTransitionID(input)
	if err != nil {
		return OperationWireRequest{}, err
	}
	return newLeaseTransitionWireRequest(OperationCompleteNoOutput, gate, input.Lease, input.Reason, transitionID)
}

func newReservationIdentityWireRequest(operation OperationName, gate TransportGate, runPolicy RunPolicyAuthority, intent ReservationIntent) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(operation, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if _, err := validateReservationIntentAgainstRunPolicy(runPolicy, intent); err != nil {
		return OperationWireRequest{}, err
	}
	reservationID, err := DeriveReservationID(runPolicy, intent)
	if err != nil {
		return OperationWireRequest{}, err
	}
	semantic, err := operationWireLeaseFields(intent.Lease)
	if err != nil {
		return OperationWireRequest{}, err
	}
	semantic = append(semantic, textField("reservation_id", string(reservationID)))
	return newOperationWireRequest(
		operation, gatePointer, semantic, nil, nil,
		operationWireReservationContext(intent, reservationID), operationWireChunkContext{},
	)
}

func newLeaseTransitionWireRequest(
	operation OperationName,
	gate TransportGate,
	lease LeaseIdentity,
	reason Reason,
	transitionID Digest,
) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(operation, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if validateNonzeroDigest(transitionID) != nil {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	semantic, err := operationWireLeaseFields(lease)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if operation != OperationReleaseBeforeIO {
		if err := ValidateTransitionReason(operation, reason); err != nil {
			return OperationWireRequest{}, err
		}
		semantic = append(semantic, textField("reason", string(reason)))
	}
	semantic = append(semantic, textField("transition_id", string(transitionID)))
	return newOperationWireRequest(
		operation, gatePointer, semantic, nil, nil,
		operationWireKeyContext{runID: lease.RunID, jobID: lease.JobID}, operationWireChunkContext{},
	)
}

func operationWireLeaseFields(lease LeaseIdentity) (Record, error) {
	if err := validateLeaseIdentity(lease); err != nil {
		return nil, err
	}
	return operationWireFields(
		textField("run_id", string(lease.RunID)),
		textField("job_id", string(lease.JobID)),
		textField("owner_id", string(lease.OwnerID)),
		textField("lease_token", string(lease.Token)),
		textField("fence", canonicalDecimal(uint64(lease.Fence))),
	), nil
}

func operationWireReservationContext(intent ReservationIntent, reservationID ReservationID) operationWireKeyContext {
	return operationWireKeyContext{
		runID:         intent.Lease.RunID,
		jobID:         intent.Lease.JobID,
		reservationID: reservationID,
		scopeIDs: []Digest{
			intent.Decision.GlobalScopeID,
			intent.Decision.GroupScopeID,
			intent.Decision.OriginScopeID,
		},
	}
}
