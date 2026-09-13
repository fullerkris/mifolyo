package crawljobsv2

import (
	"errors"
	"sort"
)

var ErrPolicyGroupBindingMismatch = errors.New("crawljobsv2: policy group binding mismatch")

// RunPinnedPolicyBindings is the complete semantic policy surface that must be
// checked against the immutable policy-group map pinned when a run is created.
// Decisions includes request decisions that are not embedded in a source or a
// discovery (for example robots, redirect, and render-resource decisions).
type RunPinnedPolicyBindings struct {
	Decisions   []PolicyDecision
	SourceJobs  []SourceJob
	Discoveries []OutputDiscovery
}

func validateRunPinnedPolicyBindings(runPolicy RunPolicyAuthority, input RunPinnedPolicyBindings) error {
	_, groups, err := authenticatedRunPolicyGroupMap(runPolicy)
	if err != nil {
		return err
	}
	for _, decision := range input.Decisions {
		if err := validateDecisionPolicyGroupBinding(decision, groups); err != nil {
			return err
		}
	}
	for _, source := range input.SourceJobs {
		if err := validateSourceJobPolicyGroupBinding(source, groups); err != nil {
			return err
		}
	}
	for _, discovery := range input.Discoveries {
		if err := validateOutputDiscoveryPolicyGroupBinding(discovery, groups); err != nil {
			return err
		}
	}
	return nil
}

func authenticatedRunPolicyGroupMap(runPolicy RunPolicyAuthority) (runPolicyBinding, map[GroupID]PolicyGroup, error) {
	binding, err := runPolicy.authenticatedBinding()
	if err != nil {
		return runPolicyBinding{}, nil, err
	}
	// authenticatedBinding verifies the complete map and its integrity before
	// this package-private read-only reference can be used by a consumer.
	return binding, runPolicy.state.policyGroups, nil
}

func validatePolicyDecisionAgainstRunPolicy(runPolicy RunPolicyAuthority, decision PolicyDecision) error {
	_, groups, err := authenticatedRunPolicyGroupMap(runPolicy)
	if err != nil {
		return err
	}
	return validateDecisionPolicyGroupBinding(decision, groups)
}

func validateSourceJobAgainstRunPolicy(runPolicy RunPolicyAuthority, source SourceJob) error {
	_, groups, err := authenticatedRunPolicyGroupMap(runPolicy)
	if err != nil {
		return err
	}
	return validateSourceJobPolicyGroupBinding(source, groups)
}

func validateSourceJobsAgainstRunPolicy(runPolicy RunPolicyAuthority, sources []SourceJob) (runPolicyBinding, error) {
	binding, groups, err := authenticatedRunPolicyGroupMap(runPolicy)
	if err != nil {
		return runPolicyBinding{}, err
	}
	for _, source := range sources {
		if err := validateSourceJobPolicyGroupBinding(source, groups); err != nil {
			return runPolicyBinding{}, err
		}
	}
	return binding, nil
}

func validateOutputDiscoveriesAgainstRunPolicy(runPolicy RunPolicyAuthority, discoveries []OutputDiscovery) error {
	_, groups, err := authenticatedRunPolicyGroupMap(runPolicy)
	if err != nil {
		return err
	}
	for _, discovery := range discoveries {
		if err := validateOutputDiscoveryPolicyGroupBinding(discovery, groups); err != nil {
			return err
		}
	}
	return nil
}

func validateReservationIntentAgainstRunPolicy(runPolicy RunPolicyAuthority, intent ReservationIntent) (runPolicyBinding, error) {
	binding, groups, err := authenticatedRunPolicyGroupMap(runPolicy)
	if err != nil {
		return runPolicyBinding{}, err
	}
	if _, err := deriveReservationIDNonAuthoritative(intent); err != nil {
		return runPolicyBinding{}, err
	}
	if intent.Lease.RunID != binding.runID || intent.CrawlPolicyDigest != binding.crawlPolicySHA256 {
		return runPolicyBinding{}, ErrPolicyGroupBindingMismatch
	}
	if err := validateDecisionPolicyGroupBinding(intent.Decision, groups); err != nil {
		return runPolicyBinding{}, err
	}
	return binding, nil
}

func validateSourceJobPolicyGroupBinding(source SourceJob, groups map[GroupID]PolicyGroup) error {
	if _, err := sourceJobRecord(source); err != nil {
		return err
	}
	return validateDecisionPolicyGroupBinding(source.Decision, groups)
}

func validateOutputDiscoveryPolicyGroupBinding(discovery OutputDiscovery, groups map[GroupID]PolicyGroup) error {
	if _, err := completeDiscoveryRecord(discovery); err != nil {
		return err
	}
	return validateDecisionPolicyGroupBinding(discovery.Decision, groups)
}

// ValidatePreRunPolicyDecisionGroupBinding is a standalone preparation check.
// It is deliberately non-authoritative: without RunPolicyAuthority it proves
// only that a decision agrees with caller-supplied group values, never that the
// values are pinned by any run.
func ValidatePreRunPolicyDecisionGroupBinding(decision PolicyDecision, groups []PolicyGroup) error {
	indexed, err := indexedPolicyGroups(groups)
	if err != nil {
		return err
	}
	return validateDecisionPolicyGroupBinding(decision, indexed)
}

// ValidatePolicyDecisionGroupBinding is the legacy name for the same
// non-authoritative, caller-supplied pre-run consistency check. It must never be
// used as evidence that a group map was pinned by a run.
//
// Deprecated: use ValidatePreRunPolicyDecisionGroupBinding to make the missing
// authority explicit at call sites.
func ValidatePolicyDecisionGroupBinding(decision PolicyDecision, groups []PolicyGroup) error {
	return ValidatePreRunPolicyDecisionGroupBinding(decision, groups)
}

func indexedPolicyGroups(groups []PolicyGroup) (map[GroupID]PolicyGroup, error) {
	if len(groups) == 0 || len(groups) > MaxPolicyGroupsPerRun {
		return nil, ErrInputLimitExceeded
	}
	ordered := append([]PolicyGroup(nil), groups...)
	sort.Slice(ordered, func(left, right int) bool {
		return string(ordered[left].GroupID) < string(ordered[right].GroupID)
	})
	indexed := make(map[GroupID]PolicyGroup, len(ordered))
	for index, group := range ordered {
		if index > 0 && group.GroupID == ordered[index-1].GroupID {
			return nil, ErrDuplicateSemanticKey
		}
		if _, err := policyGroupRecord(group); err != nil {
			return nil, err
		}
		indexed[group.GroupID] = group
	}
	return indexed, nil
}

func validateDecisionPolicyGroupBinding(decision PolicyDecision, groups map[GroupID]PolicyGroup) error {
	group, found := groups[decision.GroupID]
	if !found {
		return ErrPolicyGroupBindingMismatch
	}
	expectedGroupScopeID, err := DeriveGroupScopeID(group.RateScopeID)
	if err != nil {
		return err
	}
	if decision.GroupID != group.GroupID ||
		decision.RateScopeID != group.RateScopeID ||
		group.GroupScopeID != expectedGroupScopeID ||
		decision.GroupScopeID != expectedGroupScopeID ||
		decision.GroupConcurrency != group.Concurrency ||
		decision.GroupIntervalMS != group.IntervalMS ||
		decision.OriginConcurrency != decision.GroupConcurrency ||
		decision.OriginIntervalMS != decision.GroupIntervalMS ||
		decision.OriginConcurrency != group.Concurrency ||
		decision.OriginIntervalMS != group.IntervalMS {
		return ErrPolicyGroupBindingMismatch
	}
	if err := validatePolicyDecision(decision); err != nil {
		return err
	}
	return nil
}

// ValidateGuardCompatibility binds the two independently encoded authority
// artifacts at the only acyclic join points: Redis configuration and the exact
// guard-core digest.
func ValidateGuardCompatibility(core GuardCore, artifact CompatibilityArtifact) error {
	if err := core.validate(); err != nil {
		return err
	}
	if err := artifact.validate(); err != nil {
		return err
	}
	coreDigest, err := core.SHA256()
	if err != nil {
		return err
	}
	if artifact.CommitGuardDigest() != coreDigest || artifact.RedisConfigSHA256() != core.RedisConfigSHA256() {
		return ErrArtifactMismatch
	}
	return nil
}
