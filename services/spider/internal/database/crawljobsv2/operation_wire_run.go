package crawljobsv2

import (
	"sort"
	"strings"
)

type CreateRunWireInput struct {
	RunID                    RunID
	SourceKind               SourceKind
	SourceSHA256             Digest
	ExpectedSeedCount        uint64
	AuthorizationSHA256      Digest
	AuthorizationScopeSHA256 Digest
	AuthorizationExpiresAtMS uint64
	CanonicalizationVersion  uint64
	CanonicalizationSHA256   Digest
	CrawlPolicyVersion       uint64
	CrawlPolicySHA256        Digest
	RenderPolicyVersion      uint64
	RenderPolicySHA256       Digest
	PolicyGroupMapSHA256     Digest
	MaxJobs                  uint64
	MaxRequestStarts         uint64
	GlobalConcurrencyLimit   uint64
	MaxDeliveryAttempts      uint64
	PolicyGroups             []PolicyGroup
}

func NewCreateRunWireRequest(gate TransportGate, input CreateRunWireInput) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationCreateRun, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if validateRunID(input.RunID) != nil || validateNonzeroDigest(input.SourceSHA256) != nil ||
		validateNonzeroDigest(input.AuthorizationSHA256) != nil || validateNonzeroDigest(input.AuthorizationScopeSHA256) != nil ||
		validateNonzeroDigest(input.CanonicalizationSHA256) != nil || validateNonzeroDigest(input.CrawlPolicySHA256) != nil ||
		validateNonzeroDigest(input.RenderPolicySHA256) != nil || validateNonzeroDigest(input.PolicyGroupMapSHA256) != nil ||
		validateWirePositive(input.AuthorizationExpiresAtMS) != nil || input.ExpectedSeedCount > MaxJobsPerRun ||
		input.CanonicalizationVersion != 1 || input.CrawlPolicyVersion != 2 || input.RenderPolicyVersion == 0 ||
		input.RenderPolicyVersion > MaxExactInteger || input.MaxJobs != MaxJobsPerRun || input.MaxRequestStarts == 0 ||
		input.MaxRequestStarts > MaxRequestStartsPerRun || input.GlobalConcurrencyLimit != GlobalActiveRequestLimit ||
		input.MaxDeliveryAttempts != MaxDeliveryAttempts {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	if _, err := ParseSourceKind(string(input.SourceKind)); err != nil {
		return OperationWireRequest{}, err
	}
	if gate.mode == GateCandidate && (input.SourceKind != SourceV1Migration || input.ExpectedSeedCount == 0) ||
		gate.mode == GateActive && input.SourceKind != SourceMongo {
		return OperationWireRequest{}, ErrInvalidSourceKind
	}
	groups := append([]PolicyGroup(nil), input.PolicyGroups...)
	sort.Slice(groups, func(left, right int) bool { return string(groups[left].GroupID) < string(groups[right].GroupID) })
	if len(groups) == 0 || len(groups) > MaxPolicyGroupsPerRun {
		return OperationWireRequest{}, ErrRecordBoundsExceeded
	}
	groupDigest, err := DerivePolicyGroupMapDigest(groups)
	if err != nil || groupDigest != input.PolicyGroupMapSHA256 {
		return OperationWireRequest{}, ErrDigestInputMismatch
	}
	groupRecords := make([]Record, len(groups))
	for index, group := range groups {
		if index > 0 && group.GroupID == groups[index-1].GroupID {
			return OperationWireRequest{}, ErrDuplicateSemanticKey
		}
		groupRecords[index], err = policyGroupRecord(group)
		if err != nil {
			return OperationWireRequest{}, err
		}
	}
	return newOperationWireRequest(
		OperationCreateRun,
		gatePointer,
		operationWireFields(
			textField("run_id", string(input.RunID)),
			textField("source_kind", string(input.SourceKind)),
			textField("source_sha256", string(input.SourceSHA256)),
			textField("expected_seed_count", canonicalDecimal(input.ExpectedSeedCount)),
			textField("authorization_sha256", string(input.AuthorizationSHA256)),
			textField("authorization_scope_sha256", string(input.AuthorizationScopeSHA256)),
			textField("authorization_expires_at_ms", canonicalDecimal(input.AuthorizationExpiresAtMS)),
			textField("canonicalization_version", canonicalDecimal(input.CanonicalizationVersion)),
			textField("canonicalization_sha256", string(input.CanonicalizationSHA256)),
			textField("crawl_policy_version", canonicalDecimal(input.CrawlPolicyVersion)),
			textField("crawl_policy_sha256", string(input.CrawlPolicySHA256)),
			textField("render_policy_version", canonicalDecimal(input.RenderPolicyVersion)),
			textField("render_policy_sha256", string(input.RenderPolicySHA256)),
			textField("policy_group_map_sha256", string(input.PolicyGroupMapSHA256)),
			textField("max_jobs", canonicalDecimal(input.MaxJobs)),
			textField("max_request_starts", canonicalDecimal(input.MaxRequestStarts)),
			textField("global_concurrency_limit", canonicalDecimal(input.GlobalConcurrencyLimit)),
			textField("max_delivery_attempts", canonicalDecimal(input.MaxDeliveryAttempts)),
			textField("policy_group_count", canonicalDecimal(uint64(len(groupRecords)))),
		),
		groupRecords,
		nil,
		operationWireKeyContext{runID: input.RunID},
		operationWireChunkContext{},
	)
}

func NewEnqueueBatchWireRequest(gate TransportGate, runPolicy RunPolicyAuthority, runID RunID, jobs []SourceJob) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationEnqueueBatch, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if validateRunID(runID) != nil {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	binding, err := validateSourceJobsAgainstRunPolicy(runPolicy, jobs)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if binding.runID != runID {
		return OperationWireRequest{}, ErrPolicyGroupBindingMismatch
	}
	records, jobIDs, err := orderedCompleteSourceRecords(jobs, false)
	if err != nil {
		return OperationWireRequest{}, err
	}
	return newOperationWireRequest(
		OperationEnqueueBatch,
		gatePointer,
		operationWireFields(
			textField("run_id", string(runID)),
			textField("record_count", canonicalDecimal(uint64(len(records)))),
		),
		records,
		nil,
		operationWireKeyContext{runID: runID, recordJobIDs: jobIDs},
		operationWireChunkContext{},
	)
}

func NewBeginRunAuditWireRequest(gate TransportGate, runID RunID) (OperationWireRequest, error) {
	return newRunOnlyWireRequest(OperationBeginRunAudit, gate, runID)
}

type AuditRunBatchWireInput struct {
	RunID               RunID
	ExpectedPriorCursor JobID
	ExpectedPriorCount  uint64
	Jobs                []SourceJob
}

func NewAuditRunBatchWireRequest(gate TransportGate, runPolicy RunPolicyAuthority, input AuditRunBatchWireInput) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationAuditRunBatch, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if validateRunID(input.RunID) != nil || validateWireNonnegative(input.ExpectedPriorCount) != nil {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	if input.ExpectedPriorCursor != "" && validateJobID(input.ExpectedPriorCursor) != nil {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	binding, err := validateSourceJobsAgainstRunPolicy(runPolicy, input.Jobs)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if binding.runID != input.RunID {
		return OperationWireRequest{}, ErrPolicyGroupBindingMismatch
	}
	records, jobIDs, err := orderedCompleteSourceRecords(input.Jobs, true)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if len(jobIDs) > 0 && input.ExpectedPriorCursor != "" && string(jobIDs[0]) <= string(input.ExpectedPriorCursor) {
		return OperationWireRequest{}, ErrOperationWireRecords
	}
	return newOperationWireRequest(
		OperationAuditRunBatch,
		gatePointer,
		operationWireFields(
			textField("run_id", string(input.RunID)),
			textField("expected_prior_cursor", string(input.ExpectedPriorCursor)),
			textField("expected_prior_count", canonicalDecimal(input.ExpectedPriorCount)),
			textField("record_count", canonicalDecimal(uint64(len(records)))),
		),
		records,
		nil,
		operationWireKeyContext{runID: input.RunID, recordJobIDs: jobIDs},
		operationWireChunkContext{},
	)
}

type SealRunWireInput struct {
	RunID            RunID
	ExpectedJobCount uint64
	SourceSHA256     Digest
}

func NewSealRunWireRequest(gate TransportGate, input SealRunWireInput) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationSealRun, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if validateRunID(input.RunID) != nil || input.ExpectedJobCount > MaxJobsPerRun || validateNonzeroDigest(input.SourceSHA256) != nil {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	return newOperationWireRequest(
		OperationSealRun,
		gatePointer,
		operationWireFields(
			textField("run_id", string(input.RunID)),
			textField("expected_job_count", canonicalDecimal(input.ExpectedJobCount)),
			textField("source_sha256", string(input.SourceSHA256)),
		),
		nil,
		nil,
		operationWireKeyContext{runID: input.RunID},
		operationWireChunkContext{},
	)
}

type ActivateRunWireInput struct {
	RunID                  RunID
	SourceSHA256           Digest
	AuthorizationSHA256    Digest
	CrawlPolicySHA256      Digest
	RenderPolicySHA256     Digest
	CanonicalizationSHA256 Digest
}

func NewActivateRunWireRequest(gate TransportGate, input ActivateRunWireInput) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationActivateRun, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if gate.mode != GateActive || validateRunID(input.RunID) != nil {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	for _, digest := range []Digest{
		input.SourceSHA256, input.AuthorizationSHA256, input.CrawlPolicySHA256,
		input.RenderPolicySHA256, input.CanonicalizationSHA256,
	} {
		if validateNonzeroDigest(digest) != nil {
			return OperationWireRequest{}, ErrOperationWireArguments
		}
	}
	confirmation := strings.Join([]string{
		string(input.RunID), string(input.SourceSHA256), string(input.AuthorizationSHA256), string(input.CrawlPolicySHA256),
	}, ":")
	return newOperationWireRequest(
		OperationActivateRun,
		gatePointer,
		operationWireFields(
			textField("run_id", string(input.RunID)),
			textField("confirmation_text", confirmation),
			textField("authorization_sha256", string(input.AuthorizationSHA256)),
			textField("crawl_policy_sha256", string(input.CrawlPolicySHA256)),
			textField("render_policy_sha256", string(input.RenderPolicySHA256)),
			textField("canonicalization_sha256", string(input.CanonicalizationSHA256)),
		),
		nil,
		nil,
		operationWireKeyContext{runID: input.RunID},
		operationWireChunkContext{},
	)
}

func newRunOnlyWireRequest(operation OperationName, gate TransportGate, runID RunID) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(operation, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if validateRunID(runID) != nil {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	return newOperationWireRequest(
		operation, gatePointer, operationWireFields(textField("run_id", string(runID))), nil, nil,
		operationWireKeyContext{runID: runID}, operationWireChunkContext{},
	)
}

func orderedCompleteSourceRecords(jobs []SourceJob, allowEmpty bool) ([]Record, []JobID, error) {
	if (!allowEmpty && len(jobs) == 0) || len(jobs) > FeederEnqueueBatchSize {
		return nil, nil, ErrRecordBoundsExceeded
	}
	ordered := append([]SourceJob(nil), jobs...)
	sort.Slice(ordered, func(left, right int) bool { return string(ordered[left].JobID) < string(ordered[right].JobID) })
	records := make([]Record, len(ordered))
	jobIDs := make([]JobID, len(ordered))
	for index, job := range ordered {
		if index > 0 && job.JobID == ordered[index-1].JobID {
			return nil, nil, ErrDuplicateSourceJob
		}
		record, err := completeSourceJobRecord(job)
		if err != nil {
			return nil, nil, err
		}
		records[index] = record
		jobIDs[index] = job.JobID
	}
	return records, jobIDs, nil
}
