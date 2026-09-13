package crawljobsv2

import "strings"

type ApproveBootWireInput struct {
	CurrentRedisRunID             string
	ProposedBootEpoch             string
	EvidenceSHA256                Digest
	EvidenceAtMS                  uint64
	PlannedNonce                  string
	PlannedShutdownEvidenceSHA256 Digest
	ApprovalMode                  ApprovalMode
}

func NewApproveBootWireRequest(input ApproveBootWireInput) (OperationWireRequest, error) {
	if !isLowerHex(input.CurrentRedisRunID, 40) || !isLowerHex(input.ProposedBootEpoch, 32) ||
		validateNonzeroDigest(input.EvidenceSHA256) != nil || validateWirePositive(input.EvidenceAtMS) != nil {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	switch input.ApprovalMode {
	case ApprovalPlanned:
		if !isLowerHex(input.PlannedNonce, 32) || validateNonzeroDigest(input.PlannedShutdownEvidenceSHA256) != nil {
			return OperationWireRequest{}, ErrOperationWireArguments
		}
	case ApprovalInitial, ApprovalUncleanRehearsal:
		if input.PlannedNonce != "" || input.PlannedShutdownEvidenceSHA256 != "" {
			return OperationWireRequest{}, ErrOperationWireArguments
		}
	default:
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	return newOperationWireRequest(
		OperationApproveBoot,
		nil,
		operationWireFields(
			textField("current_redis_run_id", input.CurrentRedisRunID),
			textField("proposed_boot_epoch", input.ProposedBootEpoch),
			textField("evidence_sha256", string(input.EvidenceSHA256)),
			textField("evidence_at_ms", canonicalDecimal(input.EvidenceAtMS)),
			textField("loss_bound", "0"),
			textField("planned_nonce_or_empty", input.PlannedNonce),
			textField("planned_shutdown_evidence_sha256_or_empty", string(input.PlannedShutdownEvidenceSHA256)),
			textField("approval_mode", string(input.ApprovalMode)),
		),
		nil,
		nil,
		operationWireKeyContext{},
		operationWireChunkContext{},
	)
}

type InstallCandidateMarkersWireInput struct {
	FreezeNonce               string
	ProcessStopEvidenceSHA256 Digest
	ContractSHA256            Digest
	Compatibility             CompatibilityMarker
}

func NewInstallCandidateMarkersWireRequest(gate TransportGate, input InstallCandidateMarkersWireInput) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationInstallCandidateMarkers, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if gate.mode != GateBootOnly || !isLowerHex(input.FreezeNonce, 32) ||
		validateNonzeroDigest(input.ProcessStopEvidenceSHA256) != nil || validateNonzeroDigest(input.ContractSHA256) != nil {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	marker, err := input.Compatibility.Record()
	if err != nil {
		return OperationWireRequest{}, err
	}
	semantic := operationWireFields(
		textField("freeze_nonce", input.FreezeNonce),
		textField("process_stop_evidence_sha256", string(input.ProcessStopEvidenceSHA256)),
		textField("contract_sha256", string(input.ContractSHA256)),
	)
	semantic = append(semantic, cloneRecord(marker)...)
	return newOperationWireRequest(
		OperationInstallCandidateMarkers, gatePointer, semantic, nil, nil,
		operationWireKeyContext{}, operationWireChunkContext{},
	)
}

type RetireLegacyKeysWireInput struct {
	FreezeNonce               string
	BackupSHA256              Digest
	V1Count                   uint64
	V1URLFieldCount           uint64
	V1DepthFieldCount         uint64
	V1SourceSHA256            Digest
	V1QueueEvidenceSHA256     Digest
	V1URLsEvidenceSHA256      Digest
	V1DepthsEvidenceSHA256    Digest
	SpiderQueueType           LegacyRedisType
	SpiderQueueCount          uint64
	SpiderQueueEvidenceSHA256 Digest
	SignalQueueType           LegacyRedisType
	SignalQueueCount          uint64
	SignalQueueEvidenceSHA256 Digest
}

func NewRetireLegacyKeysWireRequest(gate TransportGate, input RetireLegacyKeysWireInput) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationRetireLegacyKeys, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if gate.mode != GateCandidate || !isLowerHex(input.FreezeNonce, 32) {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	for _, digest := range []Digest{
		input.BackupSHA256, input.V1SourceSHA256, input.V1QueueEvidenceSHA256, input.V1URLsEvidenceSHA256,
		input.V1DepthsEvidenceSHA256, input.SpiderQueueEvidenceSHA256, input.SignalQueueEvidenceSHA256,
	} {
		if validateNonzeroDigest(digest) != nil {
			return OperationWireRequest{}, ErrOperationWireArguments
		}
	}
	if input.V1Count > MaxJobsPerRun || input.V1URLFieldCount > 2*MaxJobsPerRun || input.V1DepthFieldCount > 2*MaxJobsPerRun ||
		input.V1URLFieldCount < input.V1Count || input.V1DepthFieldCount < input.V1Count ||
		!validLegacyTypeAndCount(input.SpiderQueueType, input.SpiderQueueCount, true) ||
		!validLegacyTypeAndCount(input.SignalQueueType, input.SignalQueueCount, false) {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	freeze, err := candidateGateFreeze(gate)
	if err != nil || freeze.FreezeNonce() != input.FreezeNonce {
		return OperationWireRequest{}, ErrArtifactMismatch
	}
	if gate.candidatePhase == CandidateAfterLegacyRetirement {
		legacy, err := candidateGateLegacy(gate)
		if err != nil || !retireInputMatchesRecord(input, legacy) {
			return OperationWireRequest{}, ErrArtifactMismatch
		}
	}
	confirmation := strings.Join([]string{
		input.FreezeNonce,
		string(input.BackupSHA256),
		canonicalDecimal(input.V1Count),
		string(input.V1SourceSHA256),
		string(input.V1QueueEvidenceSHA256),
		string(input.V1URLsEvidenceSHA256),
		string(input.V1DepthsEvidenceSHA256),
		string(input.SpiderQueueEvidenceSHA256),
		string(input.SignalQueueEvidenceSHA256),
	}, ":")
	return newOperationWireRequest(
		OperationRetireLegacyKeys,
		gatePointer,
		operationWireFields(
			textField("freeze_nonce", input.FreezeNonce),
			textField("backup_sha256", string(input.BackupSHA256)),
			textField("v1_count", canonicalDecimal(input.V1Count)),
			textField("v1_url_field_count", canonicalDecimal(input.V1URLFieldCount)),
			textField("v1_depth_field_count", canonicalDecimal(input.V1DepthFieldCount)),
			textField("v1_source_sha256", string(input.V1SourceSHA256)),
			textField("v1_queue_evidence_sha256", string(input.V1QueueEvidenceSHA256)),
			textField("v1_urls_evidence_sha256", string(input.V1URLsEvidenceSHA256)),
			textField("v1_depths_evidence_sha256", string(input.V1DepthsEvidenceSHA256)),
			textField("spider_queue_type", string(input.SpiderQueueType)),
			textField("spider_queue_count", canonicalDecimal(input.SpiderQueueCount)),
			textField("spider_queue_evidence_sha256", string(input.SpiderQueueEvidenceSHA256)),
			textField("signal_queue_type", string(input.SignalQueueType)),
			textField("signal_queue_count", canonicalDecimal(input.SignalQueueCount)),
			textField("signal_queue_evidence_sha256", string(input.SignalQueueEvidenceSHA256)),
			textField("confirmation_text", confirmation),
		),
		nil,
		nil,
		operationWireKeyContext{},
		operationWireChunkContext{},
	)
}

type PromoteCandidateContractsWireInput struct {
	FreezeNonce string
	GuardCore   GuardCore
}

func NewPromoteCandidateContractsWireRequest(gate TransportGate, input PromoteCandidateContractsWireInput) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationPromoteCandidateContracts, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if gate.mode != GateCandidate || gate.candidatePhase != CandidateAfterLegacyRetirement || !isLowerHex(input.FreezeNonce, 32) {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	coreRecord, err := input.GuardCore.Record()
	if err != nil {
		return OperationWireRequest{}, err
	}
	coreDigest, err := input.GuardCore.SHA256()
	if err != nil {
		return OperationWireRequest{}, err
	}
	freeze, err := candidateGateFreeze(gate)
	if err != nil || freeze.FreezeNonce() != input.FreezeNonce {
		return OperationWireRequest{}, ErrArtifactMismatch
	}
	legacy, err := candidateGateLegacy(gate)
	if err != nil {
		return OperationWireRequest{}, ErrArtifactMismatch
	}
	marker, err := DecodeCompatibilityMarker(gate.arguments[3])
	if err != nil {
		return OperationWireRequest{}, ErrArtifactMismatch
	}
	artifact, err := marker.Artifact()
	if err != nil || input.GuardCore.ContractSHA256() != Digest(gate.arguments[2]) ||
		artifact.CommitGuardDigest() != coreDigest || artifact.RedisConfigSHA256() != input.GuardCore.RedisConfigSHA256() {
		return OperationWireRequest{}, ErrArtifactMismatch
	}
	switch input.GuardCore.Cutover() {
	case CutoverFresh:
		if input.GuardCore.CandidateRun() != "" || legacy.V1Count() != 0 {
			return OperationWireRequest{}, ErrArtifactMismatch
		}
	case CutoverV1Migration:
		if legacy.V1Count() == 0 || validateRunID(input.GuardCore.CandidateRun()) != nil {
			return OperationWireRequest{}, ErrArtifactMismatch
		}
	default:
		return OperationWireRequest{}, ErrArtifactMismatch
	}
	semantic := operationWireFields(
		textField("freeze_nonce", input.FreezeNonce),
		textField("commit_guard_sha256", string(coreDigest)),
	)
	semantic = append(semantic, cloneRecord(coreRecord)...)
	return newOperationWireRequest(
		OperationPromoteCandidateContracts, gatePointer, semantic, nil, nil,
		operationWireKeyContext{runID: input.GuardCore.CandidateRun()}, operationWireChunkContext{},
	)
}

type MarkPlannedShutdownWireInput struct {
	PlannedShutdownNonce      string
	ProcessStopEvidenceSHA256 Digest
	ActiveRunIDs              []RunID
}

func NewMarkPlannedShutdownWireRequest(gate TransportGate, input MarkPlannedShutdownWireInput) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationMarkPlannedShutdown, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if gate.mode != GateActive || !isLowerHex(input.PlannedShutdownNonce, 32) ||
		validateNonzeroDigest(input.ProcessStopEvidenceSHA256) != nil || len(input.ActiveRunIDs) > MaxActiveRuns {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	runIDs, err := sortedUniqueRunIDs(input.ActiveRunIDs)
	if err != nil {
		return OperationWireRequest{}, err
	}
	repeated := make([][]byte, len(runIDs))
	for index, runID := range runIDs {
		repeated[index] = []byte(runID)
	}
	return newOperationWireRequest(
		OperationMarkPlannedShutdown,
		gatePointer,
		operationWireFields(
			textField("planned_shutdown_nonce", input.PlannedShutdownNonce),
			textField("process_stop_evidence_sha256", string(input.ProcessStopEvidenceSHA256)),
			textField("active_run_count", canonicalDecimal(uint64(len(runIDs)))),
		),
		nil,
		repeated,
		operationWireKeyContext{activeRunIDs: runIDs},
		operationWireChunkContext{},
	)
}

func candidateGateFreeze(gate TransportGate) (AdminFreezeRecord, error) {
	if gate.mode != GateCandidate || len(gate.arguments[6]) == 0 {
		return AdminFreezeRecord{}, ErrInvalidTransportGate
	}
	return DecodeAdminFreezeRecord(gate.arguments[6])
}

func candidateGateLegacy(gate TransportGate) (LegacyRetirementRecord, error) {
	if gate.mode != GateCandidate || len(gate.arguments[5]) == 0 {
		return LegacyRetirementRecord{}, ErrInvalidTransportGate
	}
	return DecodeLegacyRetirementRecord(gate.arguments[5])
}

func retireInputMatchesRecord(input RetireLegacyKeysWireInput, record LegacyRetirementRecord) bool {
	stored, err := record.Input()
	return err == nil && stored.FreezeNonce == input.FreezeNonce && stored.BackupSHA256 == input.BackupSHA256 &&
		stored.V1Count == input.V1Count && stored.V1URLFieldCount == input.V1URLFieldCount && stored.V1DepthFieldCount == input.V1DepthFieldCount &&
		stored.V1SourceSHA256 == input.V1SourceSHA256 && stored.V1QueueEvidenceSHA256 == input.V1QueueEvidenceSHA256 &&
		stored.V1URLsEvidenceSHA256 == input.V1URLsEvidenceSHA256 && stored.V1DepthsEvidenceSHA256 == input.V1DepthsEvidenceSHA256 &&
		stored.SpiderQueueType == input.SpiderQueueType && stored.SpiderQueueCount == input.SpiderQueueCount &&
		stored.SpiderQueueEvidenceSHA256 == input.SpiderQueueEvidenceSHA256 && stored.SignalQueueType == input.SignalQueueType &&
		stored.SignalQueueCount == input.SignalQueueCount && stored.SignalQueueEvidenceSHA256 == input.SignalQueueEvidenceSHA256
}
