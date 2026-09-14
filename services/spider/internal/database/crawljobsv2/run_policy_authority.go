package crawljobsv2

import (
	"fmt"
	"log/slog"
	"sort"
)

// runPolicyBinding is the immutable policy identity authenticated by an exact
// run hash read through the Redis transport boundary. A run record deliberately
// has no run_id field, so runID is supplied separately from the key identity
// used for that read.
type runPolicyBinding struct {
	runID                RunID
	crawlPolicySHA256    Digest
	renderPolicySHA256   Digest
	policyGroupCount     uint64
	policyGroupMapSHA256 Digest
}

// runPolicyAuthorityState is package-owned provenance for a RunPolicyAuthority.
// recordSHA256 binds the state to the complete validated run-record snapshot,
// while integrity detects accidental mutation of a copied package value.
type runPolicyAuthorityState struct {
	binding       runPolicyBinding
	recordSHA256  Digest
	policyGroups  map[GroupID]PolicyGroup
	integrity     Digest
	transportSeal *transportAuthoritySeal
}

// RunPolicyAuthority is opaque, immutable authority for the policy fields
// pinned on one run. Its zero value is invalid. The only production creation
// path is transportAuthority.parseRunPolicyAuthority, after an exact SchemaRun
// record has passed full value and relation validation.
//
// Value copies are safe: they retain the same immutable package-owned state and
// are revalidated at every authority-consuming boundary.
type RunPolicyAuthority struct {
	binding runPolicyBinding
	state   *runPolicyAuthorityState
}

// parseRunPolicyAuthority authenticates the policy projection of the exact run
// record and policy-group hashes returned for runID. The groups are cloned and
// indexed before their count and canonical map digest are compared with the
// fully validated run record, closing any record/groups validation-use gap.
func (transport transportAuthority) parseRunPolicyAuthority(runID RunID, record Record, groups []PolicyGroup) (RunPolicyAuthority, error) {
	if !transport.valid() {
		return RunPolicyAuthority{}, ErrInvalidResponseAuthority
	}
	if err := validateRunID(runID); err != nil {
		return RunPolicyAuthority{}, err
	}

	// Snapshot before validation so subsequent caller mutation of either input
	// cannot alter the authenticated projection or create a validation/use gap.
	snapshot := cloneRecord(record)
	groupSnapshot := append([]PolicyGroup(nil), groups...)
	if err := ValidateRecord(SchemaRun, snapshot); err != nil {
		return RunPolicyAuthority{}, err
	}
	values, err := strictTextValues(snapshot)
	if err != nil {
		return RunPolicyAuthority{}, err
	}
	crawlPolicySHA256, err := parseNonzeroDigest(values[runCrawlPolicySHA256Index])
	if err != nil {
		return RunPolicyAuthority{}, err
	}
	renderPolicySHA256, err := parseNonzeroDigest(values[runRenderPolicySHA256Index])
	if err != nil {
		return RunPolicyAuthority{}, err
	}
	policyGroupCount, err := parseCanonicalDecimal(values[runPolicyGroupCountIndex])
	if err != nil || policyGroupCount == 0 || policyGroupCount > MaxPolicyGroupsPerRun {
		return RunPolicyAuthority{}, ErrInvalidRecordValue
	}
	policyGroupMapSHA256, err := parseNonzeroDigest(values[runPolicyGroupMapSHA256Index])
	if err != nil {
		return RunPolicyAuthority{}, err
	}
	policyGroups, err := indexedPolicyGroups(groupSnapshot)
	if err != nil {
		return RunPolicyAuthority{}, err
	}
	derivedPolicyGroupMapSHA256, err := DerivePolicyGroupMapDigest(groupSnapshot)
	if err != nil {
		return RunPolicyAuthority{}, err
	}
	if uint64(len(policyGroups)) != policyGroupCount || derivedPolicyGroupMapSHA256 != policyGroupMapSHA256 {
		return RunPolicyAuthority{}, ErrPolicyGroupBindingMismatch
	}
	encoded, err := EncodeRecord(snapshot)
	if err != nil {
		return RunPolicyAuthority{}, err
	}

	binding := runPolicyBinding{
		runID:                runID,
		crawlPolicySHA256:    crawlPolicySHA256,
		renderPolicySHA256:   renderPolicySHA256,
		policyGroupCount:     policyGroupCount,
		policyGroupMapSHA256: policyGroupMapSHA256,
	}
	recordSHA256 := plainSHA256(encoded)
	ownedPolicyGroups := clonePolicyGroupMap(policyGroups)
	integrity, err := deriveRunPolicyAuthorityIntegrity(binding, recordSHA256, ownedPolicyGroups)
	if err != nil {
		return RunPolicyAuthority{}, err
	}
	state := &runPolicyAuthorityState{
		binding:       binding,
		recordSHA256:  recordSHA256,
		policyGroups:  ownedPolicyGroups,
		integrity:     integrity,
		transportSeal: transport.seal,
	}
	return RunPolicyAuthority{binding: binding, state: state}, nil
}

func (authority RunPolicyAuthority) authenticatedBinding() (runPolicyBinding, error) {
	state := authority.state
	if state == nil || state.transportSeal != &redisTransportAuthoritySeal || authority.binding != state.binding ||
		validateRunID(state.binding.runID) != nil || validateNonzeroDigest(state.binding.crawlPolicySHA256) != nil ||
		validateNonzeroDigest(state.binding.renderPolicySHA256) != nil || state.binding.policyGroupCount == 0 ||
		state.binding.policyGroupCount > MaxPolicyGroupsPerRun || validateNonzeroDigest(state.binding.policyGroupMapSHA256) != nil ||
		validateNonzeroDigest(state.recordSHA256) != nil || uint64(len(state.policyGroups)) != state.binding.policyGroupCount {
		return runPolicyBinding{}, ErrInvalidResponseAuthority
	}
	groups := policyGroupsFromMap(state.policyGroups)
	groupMapSHA256, err := DerivePolicyGroupMapDigest(groups)
	if err != nil || groupMapSHA256 != state.binding.policyGroupMapSHA256 {
		return runPolicyBinding{}, ErrInvalidResponseAuthority
	}
	integrity, err := deriveRunPolicyAuthorityIntegrity(state.binding, state.recordSHA256, state.policyGroups)
	if err != nil || state.integrity != integrity {
		return runPolicyBinding{}, ErrInvalidResponseAuthority
	}
	return state.binding, nil
}

func deriveRunPolicyAuthorityIntegrity(
	binding runPolicyBinding,
	recordSHA256 Digest,
	groups map[GroupID]PolicyGroup,
) (Digest, error) {
	groupRecords := make([]Record, 0, len(groups))
	for _, group := range policyGroupsFromMap(groups) {
		record, err := policyGroupRecord(group)
		if err != nil {
			return "", err
		}
		groupRecords = append(groupRecords, record)
	}
	groupSection, err := EncodeSection("groups", groupRecords)
	if err != nil {
		return "", err
	}
	return digestFramed(
		"mifolyo:run-policy-authority:internal:v1",
		[]byte(binding.runID),
		[]byte(binding.crawlPolicySHA256),
		[]byte(binding.renderPolicySHA256),
		[]byte(canonicalDecimal(binding.policyGroupCount)),
		[]byte(binding.policyGroupMapSHA256),
		[]byte(recordSHA256),
		groupSection,
	), nil
}

func clonePolicyGroupMap(groups map[GroupID]PolicyGroup) map[GroupID]PolicyGroup {
	cloned := make(map[GroupID]PolicyGroup, len(groups))
	for groupID, group := range groups {
		cloned[groupID] = group
	}
	return cloned
}

func policyGroupsFromMap(groups map[GroupID]PolicyGroup) []PolicyGroup {
	result := make([]PolicyGroup, 0, len(groups))
	for groupID, group := range groups {
		if group.GroupID != groupID {
			// Preserve the malformed key/value relation in the derived slice so
			// authority authentication fails during canonical map validation.
			group.GroupID = groupID
			group.RateScopeID = ""
		}
		result = append(result, group)
	}
	sort.Slice(result, func(left, right int) bool {
		return string(result[left].GroupID) < string(result[right].GroupID)
	})
	return result
}

func sameRunPolicyBinding(left, right runPolicyBinding) bool {
	return left == right
}

func (runPolicyBinding) String() string { return redactedString("runPolicyBinding") }

func (runPolicyBinding) GoString() string { return redactedString("runPolicyBinding") }

func (runPolicyBinding) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("runPolicyBinding"))
}

func (runPolicyBinding) MarshalJSON() ([]byte, error) {
	return redactedJSON("runPolicyBinding")
}

func (runPolicyBinding) MarshalText() ([]byte, error) {
	return redactedCompositeText("runPolicyBinding")
}

func (runPolicyBinding) LogValue() slog.Value {
	return redactedLogValue("runPolicyBinding")
}

func (runPolicyAuthorityState) String() string {
	return redactedString("runPolicyAuthorityState")
}

func (runPolicyAuthorityState) GoString() string {
	return redactedString("runPolicyAuthorityState")
}

func (runPolicyAuthorityState) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("runPolicyAuthorityState"))
}

func (runPolicyAuthorityState) MarshalJSON() ([]byte, error) {
	return redactedJSON("runPolicyAuthorityState")
}

func (runPolicyAuthorityState) MarshalText() ([]byte, error) {
	return redactedCompositeText("runPolicyAuthorityState")
}

func (runPolicyAuthorityState) LogValue() slog.Value {
	return redactedLogValue("runPolicyAuthorityState")
}

// Every generic diagnostic surface is intentionally lossy: run identity and
// all four pinned policy values remain available only to package authority
// checks and explicit protocol encoders.
func (RunPolicyAuthority) String() string { return redactedString("RunPolicyAuthority") }

func (RunPolicyAuthority) GoString() string { return redactedString("RunPolicyAuthority") }

func (RunPolicyAuthority) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("RunPolicyAuthority"))
}

func (RunPolicyAuthority) MarshalJSON() ([]byte, error) {
	return redactedJSON("RunPolicyAuthority")
}

func (RunPolicyAuthority) MarshalText() ([]byte, error) {
	return redactedCompositeText("RunPolicyAuthority")
}

func (RunPolicyAuthority) LogValue() slog.Value {
	return redactedLogValue("RunPolicyAuthority")
}
