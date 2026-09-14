package crawljobsv2

func NewPromoteDueWireRequest(gate TransportGate, runID RunID) (OperationWireRequest, error) {
	return newRunOnlyWireRequest(OperationPromoteDue, gate, runID)
}

func NewRecoverExpiredWireRequest(gate TransportGate, runID RunID) (OperationWireRequest, error) {
	return newRunOnlyWireRequest(OperationRecoverExpired, gate, runID)
}

type CancelRunWireInput struct {
	RunID  RunID
	Reason Reason
}

func NewCancelRunWireRequest(gate TransportGate, input CancelRunWireInput) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationCancelRun, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if validateRunID(input.RunID) != nil ||
		input.Reason != ReasonOperatorCancelled && input.Reason != ReasonSourceCancelled {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	return newOperationWireRequest(
		OperationCancelRun,
		gatePointer,
		operationWireFields(
			textField("run_id", string(input.RunID)),
			textField("reason", string(input.Reason)),
		),
		nil,
		nil,
		operationWireKeyContext{runID: input.RunID},
		operationWireChunkContext{},
	)
}

// NewAuthorizationExpiredCancelRunWireRequest is the maintenance-only path for
// an authorization-expiry cancellation. The reason is deliberately not caller
// selectable.
func NewAuthorizationExpiredCancelRunWireRequest(gate TransportGate, runID RunID) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationCancelRun, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if validateRunID(runID) != nil {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	return newOperationWireRequest(
		OperationCancelRun,
		gatePointer,
		operationWireFields(
			textField("run_id", string(runID)),
			textField("reason", string(ReasonAuthorizationExpired)),
		),
		nil,
		nil,
		operationWireKeyContext{runID: runID},
		operationWireChunkContext{},
	)
}

func NewCancelBatchWireRequest(gate TransportGate, runID RunID) (OperationWireRequest, error) {
	return newRunOnlyWireRequest(OperationCancelBatch, gate, runID)
}

func NewFinalizeRunWireRequest(gate TransportGate, runID RunID) (OperationWireRequest, error) {
	return newRunOnlyWireRequest(OperationFinalizeRun, gate, runID)
}

type ArchiveRunWireInput struct {
	RunID         RunID
	ArchiveSHA256 Digest
}

func NewArchiveRunWireRequest(gate TransportGate, input ArchiveRunWireInput) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationArchiveRun, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if validateRunID(input.RunID) != nil || validateNonzeroDigest(input.ArchiveSHA256) != nil {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	confirmation := string(input.RunID) + ":" + string(input.ArchiveSHA256)
	return newOperationWireRequest(
		OperationArchiveRun,
		gatePointer,
		operationWireFields(
			textField("run_id", string(input.RunID)),
			textField("archive_sha256", string(input.ArchiveSHA256)),
			textField("confirmation_text", confirmation),
		),
		nil,
		nil,
		operationWireKeyContext{runID: input.RunID},
		operationWireChunkContext{},
	)
}

type PurgeRunBatchWireInput struct {
	RunID              RunID
	EvidenceSHA256     Digest
	ExpectedFirstJobID JobID
}

func NewPurgeRunBatchWireRequest(gate TransportGate, input PurgeRunBatchWireInput) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationPurgeRunBatch, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if validateRunID(input.RunID) != nil || validateNonzeroDigest(input.EvidenceSHA256) != nil {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	if input.ExpectedFirstJobID != "" && validateJobID(input.ExpectedFirstJobID) != nil {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	return newOperationWireRequest(
		OperationPurgeRunBatch,
		gatePointer,
		operationWireFields(
			textField("run_id", string(input.RunID)),
			textField("evidence_sha256", string(input.EvidenceSHA256)),
			textField("expected_first_job_id_or_empty", string(input.ExpectedFirstJobID)),
		),
		nil,
		nil,
		operationWireKeyContext{runID: input.RunID, expectedJobID: input.ExpectedFirstJobID},
		operationWireChunkContext{},
	)
}

type CleanStageWireInput struct {
	ExpectedCommitID       Digest
	ExpectedCleanupDueAtMS uint64
}

func NewCleanStageWireRequest(gate TransportGate, input CleanStageWireInput) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationCleanStage, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if validateNonzeroDigest(input.ExpectedCommitID) != nil || validateWirePositive(input.ExpectedCleanupDueAtMS) != nil {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	return newOperationWireRequest(
		OperationCleanStage,
		gatePointer,
		operationWireFields(
			textField("expected_commit_id", string(input.ExpectedCommitID)),
			textField("expected_cleanup_due_at_ms", canonicalDecimal(input.ExpectedCleanupDueAtMS)),
		),
		nil,
		nil,
		operationWireKeyContext{commitID: input.ExpectedCommitID},
		operationWireChunkContext{},
	)
}

func NewMaintainRateScopesWireRequest(gate TransportGate, rankOffset uint64) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationMaintainRateScopes, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if validateWireNonnegative(rankOffset) != nil {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	return newOperationWireRequest(
		OperationMaintainRateScopes,
		gatePointer,
		operationWireFields(textField("rank_offset", canonicalDecimal(rankOffset))),
		nil,
		nil,
		operationWireKeyContext{},
		operationWireChunkContext{},
	)
}
