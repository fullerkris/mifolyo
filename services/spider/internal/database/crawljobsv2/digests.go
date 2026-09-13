package crawljobsv2

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

var (
	ErrInvalidDigestInput   = errors.New("crawljobsv2: invalid digest input")
	ErrDigestInputMismatch  = errors.New("crawljobsv2: digest input mismatch")
	ErrDuplicateSemanticKey = errors.New("crawljobsv2: duplicate semantic key")
	ErrInputLimitExceeded   = errors.New("crawljobsv2: input limit exceeded")
)

type RequestTarget struct {
	URLID        JobID
	CanonicalURL string
}

type LeaseIdentity struct {
	RunID   RunID
	JobID   JobID
	OwnerID OwnerID
	Fence   Fence
	Token   LeaseToken
}

type PolicyDecision struct {
	RequestKind       RequestKind
	TargetURLID       JobID
	TargetDigest      Digest
	Depth             uint64
	GroupID           GroupID
	RateScopeID       RateScopeID
	GlobalScopeID     Digest
	GroupScopeID      Digest
	OriginScopeID     Digest
	GlobalConcurrency uint64
	GlobalIntervalMS  uint64
	GroupConcurrency  uint64
	GroupIntervalMS   uint64
	OriginConcurrency uint64
	OriginIntervalMS  uint64
}

type PolicyDecisionInput struct {
	RequestKind       RequestKind
	Target            RequestTarget
	Depth             uint64
	GroupID           GroupID
	RateScopeID       RateScopeID
	GroupConcurrency  uint64
	GroupIntervalMS   uint64
	OriginConcurrency uint64
	OriginIntervalMS  uint64
}

type PolicyGroup struct {
	GroupID           GroupID
	RateScopeID       RateScopeID
	GroupScopeID      Digest
	RequestStartLimit uint64
	Concurrency       uint64
	IntervalMS        uint64
}

// ValidateRunPinnedPolicyBindings validates values against the immutable group
// map owned by runPolicy. It is a convenience check only: every production
// consumer revalidates its own actual input at the consumption boundary.
func ValidateRunPinnedPolicyBindings(runPolicy RunPolicyAuthority, input RunPinnedPolicyBindings) error {
	return validateRunPinnedPolicyBindings(runPolicy, input)
}

type ReservationIntent struct {
	Lease             LeaseIdentity
	RequestOrdinal    uint64
	Target            RequestTarget
	CrawlPolicyDigest Digest
	Decision          PolicyDecision
}

type PublicationIdentity struct {
	RunID        RunID
	JobID        JobID
	Fence        Fence
	OutputDigest Digest
}

type CommitIdentity struct {
	RunID                   RunID
	JobID                   JobID
	OwnerID                 OwnerID
	Fence                   Fence
	Token                   LeaseToken
	PublicationID           Digest
	RequestStartsBaseline   uint64
	RequestStartsGeneration uint64
}

func DeriveGlobalScopeID() Digest {
	return digestFramed("mifolyo:rate:global:v2")
}

func DeriveGroupScopeID(rateScopeID RateScopeID) (Digest, error) {
	if err := validateRateScopeID(rateScopeID); err != nil {
		return "", err
	}
	return digestFramed("mifolyo:rate:group:v2", []byte(rateScopeID)), nil
}

func DeriveOriginScopeID(origin CanonicalOrigin) (Digest, error) {
	if err := validateCanonicalOrigin(origin); err != nil {
		return "", err
	}
	return digestFramed("mifolyo:rate:origin:v2", []byte(origin)), nil
}

func DeriveTargetDigest(target RequestTarget) (Digest, error) {
	if err := validateRequestTarget(target); err != nil {
		return "", err
	}
	return digestFramed(
		"mifolyo:request-target:v2",
		[]byte(target.URLID),
		[]byte(target.CanonicalURL),
	), nil
}

// NewPolicyDecision derives every target and scope identity from semantic
// inputs. This is a non-authoritative pre-run builder: a run consumer must also
// validate the result against RunPolicyAuthority's pinned group map.
func NewPolicyDecision(input PolicyDecisionInput) (PolicyDecision, error) {
	targetDigest, err := DeriveTargetDigest(input.Target)
	if err != nil {
		return PolicyDecision{}, err
	}
	groupScopeID, err := DeriveGroupScopeID(input.RateScopeID)
	if err != nil {
		return PolicyDecision{}, err
	}
	origin, err := DeriveCanonicalOrigin(input.Target.CanonicalURL)
	if err != nil {
		return PolicyDecision{}, err
	}
	originScopeID, err := DeriveOriginScopeID(origin)
	if err != nil {
		return PolicyDecision{}, err
	}
	decision := PolicyDecision{
		RequestKind:       input.RequestKind,
		TargetURLID:       input.Target.URLID,
		TargetDigest:      targetDigest,
		Depth:             input.Depth,
		GroupID:           input.GroupID,
		RateScopeID:       input.RateScopeID,
		GlobalScopeID:     DeriveGlobalScopeID(),
		GroupScopeID:      groupScopeID,
		OriginScopeID:     originScopeID,
		GlobalConcurrency: GlobalActiveRequestLimit,
		GlobalIntervalMS:  GlobalScopeIntervalMilliseconds,
		GroupConcurrency:  input.GroupConcurrency,
		GroupIntervalMS:   input.GroupIntervalMS,
		OriginConcurrency: input.OriginConcurrency,
		OriginIntervalMS:  input.OriginIntervalMS,
	}
	if err := validatePolicyDecision(decision); err != nil {
		return PolicyDecision{}, err
	}
	return decision, nil
}

func DeriveTokenDigest(lease LeaseIdentity) (Digest, error) {
	if err := validateLeaseIdentity(lease); err != nil {
		return "", err
	}
	return digestFramed(
		"mifolyo:lease-token:v2",
		[]byte(lease.RunID),
		[]byte(lease.JobID),
		[]byte(canonicalDecimal(uint64(lease.Fence))),
		[]byte(lease.Token),
	), nil
}

// DerivePolicyDecisionDigest is a non-authoritative pre-run serialization
// helper. It validates internal structure, not membership in any run policy.
func DerivePolicyDecisionDigest(decision PolicyDecision) (Digest, error) {
	record, err := policyDecisionRecord(decision)
	if err != nil {
		return "", err
	}
	section, err := EncodeSection("decision", []Record{record})
	if err != nil {
		return "", err
	}
	return digestEncoded("mifolyo:policy-decision:v2", section), nil
}

// DerivePolicyGroupMapDigest is a non-authoritative pre-run digest. Run policy
// authority is created only after transport parsing compares this canonical
// digest and count with the fully validated run record.
func DerivePolicyGroupMapDigest(groups []PolicyGroup) (Digest, error) {
	if len(groups) == 0 || len(groups) > MaxPolicyGroupsPerRun {
		return "", ErrInputLimitExceeded
	}
	ordered := append([]PolicyGroup(nil), groups...)
	sort.Slice(ordered, func(left, right int) bool {
		return string(ordered[left].GroupID) < string(ordered[right].GroupID)
	})
	records := make([]Record, 0, len(ordered))
	for index, group := range ordered {
		if index > 0 && group.GroupID == ordered[index-1].GroupID {
			return "", ErrDuplicateSemanticKey
		}
		record, err := policyGroupRecord(group)
		if err != nil {
			return "", err
		}
		records = append(records, record)
	}
	section, err := EncodeSection("groups", records)
	if err != nil {
		return "", err
	}
	return digestEncoded("mifolyo:policy-group-map:v2", section), nil
}

// DeriveReservationID validates intent against the exact run and policy-group
// map before deriving the normative reservation identity.
func DeriveReservationID(runPolicy RunPolicyAuthority, intent ReservationIntent) (ReservationID, error) {
	if _, err := validateReservationIntentAgainstRunPolicy(runPolicy, intent); err != nil {
		return "", err
	}
	return deriveReservationIDNonAuthoritative(intent)
}

// deriveReservationIDNonAuthoritative contains only the normative digest
// formula and structural relations. It is deliberately unexported so a
// run-bearing ReservationIntent cannot bypass RunPolicyAuthority validation.
func deriveReservationIDNonAuthoritative(intent ReservationIntent) (ReservationID, error) {
	if err := validateLeaseIdentity(intent.Lease); err != nil {
		return "", err
	}
	if intent.RequestOrdinal == 0 || intent.RequestOrdinal > MaxReservationCreationsPerRun {
		return "", ErrInvalidUnsignedDecimal
	}
	if err := validateNonzeroDigest(intent.CrawlPolicyDigest); err != nil {
		return "", err
	}
	targetDigest, err := DeriveTargetDigest(intent.Target)
	if err != nil {
		return "", err
	}
	if intent.Decision.TargetURLID != intent.Target.URLID || intent.Decision.TargetDigest != targetDigest {
		return "", ErrDigestInputMismatch
	}
	decisionDigest, err := DerivePolicyDecisionDigest(intent.Decision)
	if err != nil {
		return "", err
	}
	origin, err := DeriveCanonicalOrigin(intent.Target.CanonicalURL)
	if err != nil {
		return "", err
	}
	originScopeID, err := DeriveOriginScopeID(origin)
	if err != nil {
		return "", err
	}
	if intent.Decision.OriginScopeID != originScopeID {
		return "", ErrDigestInputMismatch
	}

	decision := intent.Decision
	digest := digestFramed(
		"mifolyo:request-reservation:v2",
		[]byte(intent.Lease.RunID),
		[]byte(intent.Lease.JobID),
		[]byte(canonicalDecimal(uint64(intent.Lease.Fence))),
		[]byte(intent.Lease.Token),
		[]byte(canonicalDecimal(intent.RequestOrdinal)),
		[]byte(decision.RequestKind),
		[]byte(intent.Target.URLID),
		[]byte(targetDigest),
		[]byte(intent.CrawlPolicyDigest),
		[]byte(decisionDigest),
		[]byte(decision.GroupID),
		[]byte(decision.RateScopeID),
		[]byte(decision.GlobalScopeID),
		[]byte(decision.GroupScopeID),
		[]byte(decision.OriginScopeID),
		[]byte(canonicalDecimal(decision.GlobalConcurrency)),
		[]byte("0"),
		[]byte(canonicalDecimal(decision.GroupConcurrency)),
		[]byte(canonicalDecimal(decision.GroupIntervalMS)),
		[]byte(canonicalDecimal(decision.OriginConcurrency)),
		[]byte(canonicalDecimal(decision.OriginIntervalMS)),
	)
	return ReservationID(digest), nil
}

func DerivePublicationID(publication PublicationIdentity) (Digest, error) {
	if err := validateRunID(publication.RunID); err != nil {
		return "", err
	}
	if err := validateJobID(publication.JobID); err != nil {
		return "", err
	}
	if _, err := publication.Fence.Decimal(); err != nil {
		return "", err
	}
	if err := validateNonzeroDigest(publication.OutputDigest); err != nil {
		return "", err
	}
	return digestFramed(
		"mifolyo:page-publication:v2",
		[]byte(publication.RunID),
		[]byte(publication.JobID),
		[]byte(canonicalDecimal(uint64(publication.Fence))),
		[]byte(publication.OutputDigest),
	), nil
}

// DeriveCommitID is a pure identity formula, not output or live-stage authority.
// The transcript tuple is mandatory; there is no legacy/default generation.
func DeriveCommitID(commit CommitIdentity) (Digest, error) {
	lease := LeaseIdentity{RunID: commit.RunID, JobID: commit.JobID, OwnerID: commit.OwnerID, Fence: commit.Fence, Token: commit.Token}
	if err := validateLeaseIdentity(lease); err != nil {
		return "", err
	}
	if err := validateNonzeroDigest(commit.PublicationID); err != nil {
		return "", err
	}
	if err := validateRequestStartsTuple(commit.RequestStartsBaseline, commit.RequestStartsGeneration); err != nil {
		return "", err
	}
	return digestFramed(
		"mifolyo:crawl-commit:v2",
		[]byte(commit.RunID),
		[]byte(commit.JobID),
		[]byte(canonicalDecimal(uint64(commit.Fence))),
		[]byte(commit.Token),
		[]byte(commit.PublicationID),
		[]byte(canonicalDecimal(commit.RequestStartsBaseline)),
		[]byte(canonicalDecimal(commit.RequestStartsGeneration)),
	), nil
}

func validateRequestStartsTuple(baseline, generation uint64) error {
	if baseline >= generation || generation > MaxRequestStartsPerRun {
		return ErrDigestInputMismatch
	}
	return nil
}

func policyDecisionRecord(decision PolicyDecision) (Record, error) {
	if err := validatePolicyDecision(decision); err != nil {
		return nil, err
	}
	return Record{
		textField("request_kind", string(decision.RequestKind)),
		textField("target_url_id", string(decision.TargetURLID)),
		textField("target_digest", string(decision.TargetDigest)),
		textField("depth", canonicalDecimal(decision.Depth)),
		textField("group_id", string(decision.GroupID)),
		textField("rate_scope_id", string(decision.RateScopeID)),
		textField("global_scope_id", string(decision.GlobalScopeID)),
		textField("group_scope_id", string(decision.GroupScopeID)),
		textField("origin_scope_id", string(decision.OriginScopeID)),
		textField("global_concurrency", canonicalDecimal(decision.GlobalConcurrency)),
		textField("global_interval_ms", canonicalDecimal(decision.GlobalIntervalMS)),
		textField("group_concurrency", canonicalDecimal(decision.GroupConcurrency)),
		textField("group_interval_ms", canonicalDecimal(decision.GroupIntervalMS)),
		textField("origin_concurrency", canonicalDecimal(decision.OriginConcurrency)),
		textField("origin_interval_ms", canonicalDecimal(decision.OriginIntervalMS)),
	}, nil
}

func validatePolicyDecision(decision PolicyDecision) error {
	if err := validateRequestKind(decision.RequestKind); err != nil {
		return err
	}
	if err := validateJobID(decision.TargetURLID); err != nil {
		return err
	}
	if err := validateNonzeroDigest(decision.TargetDigest); err != nil {
		return err
	}
	if err := validateNonnegativeExactInteger(decision.Depth); err != nil {
		return err
	}
	if err := validateGroupID(decision.GroupID); err != nil {
		return err
	}
	if err := validateRateScopeID(decision.RateScopeID); err != nil {
		return err
	}
	for _, scopeID := range []Digest{decision.GlobalScopeID, decision.GroupScopeID, decision.OriginScopeID} {
		if err := validateNonzeroDigest(scopeID); err != nil {
			return err
		}
	}
	expectedGroupScopeID, err := DeriveGroupScopeID(decision.RateScopeID)
	if err != nil {
		return err
	}
	if decision.GlobalScopeID != DeriveGlobalScopeID() || decision.GroupScopeID != expectedGroupScopeID {
		return ErrDigestInputMismatch
	}
	if decision.GlobalScopeID == decision.GroupScopeID || decision.GlobalScopeID == decision.OriginScopeID || decision.GroupScopeID == decision.OriginScopeID {
		return ErrDigestInputMismatch
	}
	if decision.GlobalConcurrency != GlobalActiveRequestLimit || decision.GlobalIntervalMS != GlobalScopeIntervalMilliseconds {
		return ErrInvalidDigestInput
	}
	if !validScopeTuple(decision.GroupConcurrency, decision.GroupIntervalMS) || !validScopeTuple(decision.OriginConcurrency, decision.OriginIntervalMS) {
		return ErrInvalidDigestInput
	}
	if decision.GroupConcurrency != decision.OriginConcurrency || decision.GroupIntervalMS != decision.OriginIntervalMS {
		return ErrDigestInputMismatch
	}
	return nil
}

func policyGroupRecord(group PolicyGroup) (Record, error) {
	if err := validateGroupID(group.GroupID); err != nil {
		return nil, err
	}
	if err := validateRateScopeID(group.RateScopeID); err != nil {
		return nil, err
	}
	if err := validateNonzeroDigest(group.GroupScopeID); err != nil {
		return nil, err
	}
	expectedScopeID, err := DeriveGroupScopeID(group.RateScopeID)
	if err != nil {
		return nil, err
	}
	if group.GroupScopeID != expectedScopeID {
		return nil, ErrDigestInputMismatch
	}
	if group.RequestStartLimit < MinRequestStartsPerGroup || group.RequestStartLimit > MaxRequestStartsPerGroup || !validScopeTuple(group.Concurrency, group.IntervalMS) {
		return nil, ErrInvalidDigestInput
	}
	return Record{
		textField("group_id", string(group.GroupID)),
		textField("rate_scope_id", string(group.RateScopeID)),
		textField("group_scope_id", string(group.GroupScopeID)),
		textField("request_start_limit", canonicalDecimal(group.RequestStartLimit)),
		textField("concurrency", canonicalDecimal(group.Concurrency)),
		textField("interval_ms", canonicalDecimal(group.IntervalMS)),
	}, nil
}

func validateRequestTarget(target RequestTarget) error {
	if err := validateJobID(target.URLID); err != nil {
		return err
	}
	identity, err := requireCanonicalURL(target.CanonicalURL)
	if err != nil {
		return err
	}
	if identity.URLID != string(target.URLID) {
		return ErrURLIdentityMismatch
	}
	return nil
}

func validateLeaseIdentity(lease LeaseIdentity) error {
	if err := validateRunID(lease.RunID); err != nil {
		return err
	}
	if err := validateJobID(lease.JobID); err != nil {
		return err
	}
	if err := validateOwnerID(lease.OwnerID); err != nil {
		return err
	}
	if _, err := lease.Fence.Decimal(); err != nil {
		return err
	}
	return validateLeaseToken(lease.Token)
}

func deriveTransitionPayloadDigest(record Record) (Digest, error) {
	section, err := EncodeSection("arguments", []Record{record})
	if err != nil {
		return "", err
	}
	if len(section) > MaxOrdinaryEvalSHARequestBytes {
		return "", ErrInputLimitExceeded
	}
	return digestEncoded("mifolyo:transition-payload:v2", section), nil
}

func deriveTransitionID(operation OperationName, runID RunID, jobID JobID, fence Fence, token LeaseToken, reason Reason, payload Record) (Digest, error) {
	if _, err := ParseOperationName(string(operation)); err != nil {
		return "", err
	}
	if err := validateRunID(runID); err != nil {
		return "", err
	}
	if err := validateJobID(jobID); err != nil {
		return "", err
	}
	if _, err := ParseReason(string(reason)); err != nil {
		return "", err
	}

	fenceText := "0"
	tokenText := ""
	if fence == 0 {
		if token != "" {
			return "", ErrDigestInputMismatch
		}
	} else {
		if _, err := fence.Decimal(); err != nil {
			return "", err
		}
		if err := validateLeaseToken(token); err != nil {
			return "", err
		}
		fenceText = canonicalDecimal(uint64(fence))
		tokenText = string(token)
	}

	payloadDigest, err := deriveTransitionPayloadDigest(payload)
	if err != nil {
		return "", err
	}
	return digestFramed(
		"mifolyo:crawl-transition:v2",
		[]byte(operation),
		[]byte(runID),
		[]byte(jobID),
		[]byte(fenceText),
		[]byte(tokenText),
		[]byte(reason),
		[]byte(payloadDigest),
	), nil
}

func validScopeTuple(concurrency, intervalMS uint64) bool {
	return concurrency >= 1 && concurrency <= MaxScopeConcurrency && intervalMS <= MaxScopeIntervalMilliseconds
}

func requireCanonicalURL(value string) (utils.CanonicalizedURL, error) {
	identity, err := utils.CanonicalizeURLV1(value)
	if err != nil || identity.CanonicalURL != value {
		return utils.CanonicalizedURL{}, ErrInvalidCanonicalURL
	}
	return identity, nil
}

func textField(name, value string) Field {
	return Field{Name: name, Value: []byte(value)}
}

func digestFramed(domain string, values ...[]byte) Digest {
	hash := sha256.New()
	_, _ = hash.Write(F([]byte(domain)))
	for _, value := range values {
		_, _ = hash.Write(F(value))
	}
	return Digest(hex.EncodeToString(hash.Sum(nil)))
}

func digestEncoded(domain string, encodedSections ...[]byte) Digest {
	hash := sha256.New()
	_, _ = hash.Write(F([]byte(domain)))
	for _, section := range encodedSections {
		_, _ = hash.Write(section)
	}
	return Digest(hex.EncodeToString(hash.Sum(nil)))
}
