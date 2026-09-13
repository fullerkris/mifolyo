package crawljobsv2

// These are pure, context-bearing ledger prevalidation checks, not a simulator
// or a source of OutputContext authority. Callers must supply exact validated
// records from one authoritative snapshot. The eventual Lua transitions must
// repeat these predicates atomically with their writes; client reads do not
// freeze a lease or establish freshness across a round trip.

func validateJobOutputTranscript(job Record, context OutputContext) error {
	if err := ValidateRecord(SchemaJob, job); err != nil {
		return err
	}
	if err := validateOutputContextAuthority(context); err != nil {
		return err
	}
	lease := context.lease
	sourceIntent, err := authenticatedRequestIntent(*context.sourceRequest)
	if err != nil {
		return err
	}
	decisionDigest, err := DerivePolicyDecisionDigest(sourceIntent.Decision)
	if err != nil {
		return err
	}
	witness := context.witness
	if string(job[jobStateIndex].Value) != "leased" ||
		string(job[jobRunIDIndex].Value) != string(lease.RunID) ||
		string(job[jobJobIDIndex].Value) != string(lease.JobID) ||
		string(job[jobLeaseOwnerIndex].Value) != string(lease.OwnerID) ||
		string(job[jobLeaseTokenIndex].Value) != string(lease.Token) ||
		string(job[jobLeaseFenceIndex].Value) != canonicalDecimal(uint64(lease.Fence)) ||
		string(job[jobLastDocumentRequestFenceIndex].Value) != canonicalDecimal(uint64(lease.Fence)) ||
		len(job[jobActiveReservationIDIndex].Value) != 0 ||
		string(job[jobLeaseRequestStartsBaselineIndex].Value) != canonicalDecimal(context.requestStartsBaseline) ||
		string(job[jobRequestStartsIndex].Value) != canonicalDecimal(context.requestStartsGeneration) {
		return ErrRecordRelation
	}
	if string(job[jobCanonicalURLIndex].Value) != sourceIntent.Target.CanonicalURL ||
		string(job[jobURLIDIndex].Value) != string(sourceIntent.Target.URLID) ||
		string(job[jobDepthIndex].Value) != canonicalDecimal(sourceIntent.Decision.Depth) ||
		string(job[jobGroupIDIndex].Value) != string(sourceIntent.Decision.GroupID) ||
		string(job[jobRateScopeIDIndex].Value) != string(sourceIntent.Decision.RateScopeID) ||
		string(job[jobGroupScopeIDIndex].Value) != string(sourceIntent.Decision.GroupScopeID) ||
		string(job[jobInitialOriginScopeIDIndex].Value) != string(sourceIntent.Decision.OriginScopeID) ||
		string(job[jobPolicyDecisionSHA256Index].Value) != string(decisionDigest) ||
		string(job[jobDeliveryAttemptsIndex].Value) != canonicalDecimal(context.sourceRequest.deliveryAttempts) ||
		string(job[jobLastDocumentRequestStartedAtMSIndex].Value) != canonicalDecimal(uint64(witness.redisStartedAtMS)) ||
		string(job[jobLastRequestStartedAtMSIndex].Value) != canonicalDecimal(uint64(witness.terminalRequestStartedAtMS)) ||
		string(job[jobLastDocumentTargetURLIDIndex].Value) != string(witness.target.URLID) ||
		string(job[jobLastDocumentTargetURLIndex].Value) != witness.target.CanonicalURL ||
		string(job[jobLastDocumentTargetDigestIndex].Value) != string(witness.targetDigest) {
		return ErrRecordRelation
	}
	return nil
}

// ValidateBeginStageTranscript validates a FIRST BEGIN against a live job,
// including freshness since the witness read. Exact existing-stage replays use
// ValidateStageTranscript instead. An aborted stage cannot reopen this fence.
func ValidateBeginStageTranscript(job Record, input BeginStageWireInput) error {
	if _, _, err := prepareBeginStage(input); err != nil {
		return err
	}
	if err := validateJobOutputTranscript(job, input.Context); err != nil {
		return err
	}
	if len(job[jobActiveStageCommitIDIndex].Value) != 0 ||
		string(job[jobLastStageFenceIndex].Value) == canonicalDecimal(uint64(input.Lease.Fence)) {
		return ErrRecordRelation
	}
	return nil
}

// ValidateStageTranscript checks the frozen full lease/B/G tuple for existing
// BEGIN replay, every stage write, seal, and FIRST commit. Other predicates
// (expiry, slot/index membership, byte replay, counts, verified chunk digests,
// budgets and memory) remain the responsibility of the eventual transition.
func ValidateStageTranscript(operation OperationName, job, stage Record, context OutputContext, identity CommitIdentity) error {
	switch operation {
	case OperationBeginStage, OperationStagePageFields, OperationStagePageBlob, OperationStageOutlinksBatch,
		OperationStageDiscoveriesBatch, OperationStageAliasesBatch, OperationStageImagesBatch,
		OperationStageImageManifest, OperationSealStage, OperationCommit:
	default:
		return ErrUnknownOperation
	}
	if err := validateJobOutputTranscript(job, context); err != nil {
		return err
	}
	if err := validateOutputCommitIdentity(context, identity); err != nil {
		return err
	}
	if err := ValidateRecord(SchemaStageMeta, stage); err != nil {
		return err
	}
	commitID, err := DeriveCommitID(identity)
	if err != nil {
		return err
	}
	tokenDigest, err := DeriveTokenDigest(context.lease)
	if err != nil {
		return err
	}
	if string(stage[stageRunIDIndex].Value) != string(identity.RunID) ||
		string(stage[stageJobIDIndex].Value) != string(identity.JobID) ||
		string(stage[stageOwnerIDIndex].Value) != string(identity.OwnerID) ||
		string(stage[stageLeaseFenceIndex].Value) != canonicalDecimal(uint64(identity.Fence)) ||
		string(stage[stageTokenDigestIndex].Value) != string(tokenDigest) ||
		string(stage[stagePublicationIDIndex].Value) != string(identity.PublicationID) ||
		string(stage[stageCommitIDIndex].Value) != string(commitID) ||
		string(stage[stageRequestStartsBaselineIndex].Value) != canonicalDecimal(identity.RequestStartsBaseline) ||
		string(stage[stageRequestStartsGenerationIndex].Value) != canonicalDecimal(identity.RequestStartsGeneration) ||
		string(stage[stageAbandonedIndex].Value) != "0" ||
		string(job[jobActiveStageCommitIDIndex].Value) != string(commitID) ||
		string(job[jobLastStageCommitIDIndex].Value) != string(commitID) ||
		string(job[jobLastStageFenceIndex].Value) != canonicalDecimal(uint64(identity.Fence)) {
		return ErrRecordRelation
	}
	if operation == OperationCommit && string(stage[stageSealedIndex].Value) != "1" {
		return ErrRecordRelation
	}
	return nil
}

// ValidateRequestTranscriptMutable is the shared RESERVE/START freeze predicate.
// last_stage_fence, not the active pointer, keeps it closed after ABORT_STAGE.
func ValidateRequestTranscriptMutable(job Record, lease LeaseIdentity) error {
	if err := ValidateRecord(SchemaJob, job); err != nil {
		return err
	}
	if validateLeaseIdentity(lease) != nil || string(job[jobStateIndex].Value) != "leased" ||
		string(job[jobRunIDIndex].Value) != string(lease.RunID) || string(job[jobJobIDIndex].Value) != string(lease.JobID) ||
		string(job[jobLeaseOwnerIndex].Value) != string(lease.OwnerID) || string(job[jobLeaseTokenIndex].Value) != string(lease.Token) ||
		string(job[jobLeaseFenceIndex].Value) != canonicalDecimal(uint64(lease.Fence)) ||
		string(job[jobLastStageFenceIndex].Value) == canonicalDecimal(uint64(lease.Fence)) {
		return ErrRecordRelation
	}
	return nil
}

// ValidateJobRequestStartsTransition checks ONLY counter/baseline/freeze evolution
// of two complete records, not the rest of a transition's authorization. A new
// claim snapshots the cumulative starts; every other operation retains B. G
// increases only at a new START, which is forbidden on a frozen fence.
func ValidateJobRequestStartsTransition(operation OperationName, before, after Record) error {
	if _, err := ParseOperationName(string(operation)); err != nil {
		return err
	}
	for _, record := range []Record{before, after} {
		if err := ValidateRecord(SchemaJob, record); err != nil {
			return err
		}
	}
	for _, index := range []int{jobRunIDIndex, jobJobIDIndex} {
		if string(before[index].Value) != string(after[index].Value) {
			return ErrRecordRelation
		}
	}
	priorG, _ := parseResponseUint(string(before[jobRequestStartsIndex].Value))
	currentG, _ := parseResponseUint(string(after[jobRequestStartsIndex].Value))
	priorFence, _ := parseResponseUint(string(before[jobLeaseFenceIndex].Value))
	currentFence, _ := parseResponseUint(string(after[jobLeaseFenceIndex].Value))
	priorStageFence, _ := parseResponseUint(string(before[jobLastStageFenceIndex].Value))
	currentStageFence, _ := parseResponseUint(string(after[jobLastStageFenceIndex].Value))
	if currentStageFence < priorStageFence ||
		currentStageFence == priorStageFence && string(before[jobLastStageCommitIDIndex].Value) != string(after[jobLastStageCommitIDIndex].Value) {
		return ErrRecordRelation
	}
	if currentStageFence != priorStageFence &&
		(operation != OperationBeginStage || currentFence != priorFence || currentStageFence != currentFence) {
		return ErrRecordRelation
	}
	if currentFence != priorFence {
		if operation != OperationTryClaim || currentFence != priorFence+1 || priorG >= MaxRequestStartsPerRun ||
			currentG != priorG || string(after[jobStateIndex].Value) != "leased" ||
			string(before[jobStateIndex].Value) != "ready" ||
			string(after[jobLeaseRequestStartsBaselineIndex].Value) != canonicalDecimal(priorG) {
			return ErrRecordRelation
		}
		return nil
	}
	if string(before[jobLeaseRequestStartsBaselineIndex].Value) != string(after[jobLeaseRequestStartsBaselineIndex].Value) {
		return ErrRecordRelation
	}
	if currentG != priorG {
		lease := LeaseIdentity{
			RunID: RunID(before[jobRunIDIndex].Value), JobID: JobID(before[jobJobIDIndex].Value),
			OwnerID: OwnerID(before[jobLeaseOwnerIndex].Value), Token: LeaseToken(before[jobLeaseTokenIndex].Value), Fence: Fence(priorFence),
		}
		if operation != OperationStartRequest || currentG != priorG+1 || ValidateRequestTranscriptMutable(before, lease) != nil {
			return ErrRecordRelation
		}
	}
	return nil
}

// ValidateCompletedCommitReplay binds retained B/G and the caller's original
// token/fence to the completed job. It deliberately requires neither live lease
// fields (cleared at completion) nor stage keys (eligible for cleanup).
func ValidateCompletedCommitReplay(job Record, identity CommitIdentity) error {
	if err := ValidateRecord(SchemaJob, job); err != nil {
		return err
	}
	commitID, err := DeriveCommitID(identity)
	if err != nil {
		return err
	}
	if string(job[jobStateIndex].Value) != "completed" || string(job[jobLastReasonIndex].Value) != string(ReasonPublished) ||
		string(job[jobRunIDIndex].Value) != string(identity.RunID) || string(job[jobJobIDIndex].Value) != string(identity.JobID) ||
		string(job[jobLeaseFenceIndex].Value) != canonicalDecimal(uint64(identity.Fence)) ||
		string(job[jobLeaseRequestStartsBaselineIndex].Value) != canonicalDecimal(identity.RequestStartsBaseline) ||
		string(job[jobRequestStartsIndex].Value) != canonicalDecimal(identity.RequestStartsGeneration) ||
		string(job[jobPublicationIDIndex].Value) != string(identity.PublicationID) || string(job[jobCommitIDIndex].Value) != string(commitID) {
		return ErrRecordRelation
	}
	return nil
}
