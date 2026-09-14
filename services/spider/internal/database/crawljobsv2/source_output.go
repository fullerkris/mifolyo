package crawljobsv2

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/IonelPopJara/search-engine/services/spider/internal/renderpolicy"
)

var (
	ErrDuplicateSourceJob = errors.New("crawljobsv2: duplicate source job")
	ErrInvalidOutput      = errors.New("crawljobsv2: invalid output")

	ErrDuplicateOutlink   error = newOutputRejectionError("DUPLICATE_OUTLINK", "crawljobsv2: duplicate outlink")
	ErrDuplicateImage     error = newOutputRejectionError("DUPLICATE_IMAGE", "crawljobsv2: duplicate image")
	ErrDuplicateDiscovery error = newOutputRejectionError("DUPLICATE_DISCOVERY", "crawljobsv2: duplicate discovery")
	ErrDuplicateAlias     error = newOutputRejectionError("DUPLICATE_ALIAS", "crawljobsv2: duplicate alias")

	ErrOutputCountLimit                error = newOutputRejectionError("OUTPUT_COUNT_LIMIT", "crawljobsv2: output count limit", ErrInputLimitExceeded)
	ErrOutputAliasCountLimit           error = newOutputRejectionError("ALIAS_COUNT_LIMIT", "crawljobsv2: output alias count limit", ErrInputLimitExceeded)
	ErrOutputPageBlobLimit             error = newOutputRejectionError("PAGE_BLOB_LIMIT", "crawljobsv2: output page blob limit", ErrInvalidOutput)
	ErrOutputCombinedHTMLLimit         error = newOutputRejectionError("COMBINED_HTML_LIMIT", "crawljobsv2: output combined HTML limit", ErrInvalidOutput)
	ErrOutputImageAltLimit             error = newOutputRejectionError("IMAGE_ALT_LIMIT", "crawljobsv2: output image alt limit", ErrInvalidOutput)
	ErrOutputContentTypeLimit          error = newOutputRejectionError("CONTENT_TYPE_LIMIT", "crawljobsv2: output content type limit", ErrInvalidOutput)
	ErrOutputInvalidContentType        error = newOutputRejectionError("INVALID_CONTENT_TYPE", "crawljobsv2: invalid output content type", ErrInvalidOutput)
	ErrOutputStatusCodeRange           error = newOutputRejectionError("STATUS_CODE_RANGE", "crawljobsv2: output status code out of range", ErrInvalidOutput)
	ErrOutputInvalidRenderRelation     error = newOutputRejectionError("INVALID_RENDER_RELATION", "crawljobsv2: invalid output render relation", ErrInvalidOutput)
	ErrOutputRenderRuleLimit           error = newOutputRejectionError("RENDER_RULE_LIMIT", "crawljobsv2: output render rule limit", ErrInvalidOutput)
	ErrOutputInvalidRenderPolicyDigest error = newOutputRejectionError("INVALID_RENDER_POLICY_DIGEST", "crawljobsv2: invalid output render-policy digest", ErrDigestInputMismatch)
	ErrOutputInvalidUTF8               error = newOutputRejectionError("INVALID_UTF8", "crawljobsv2: invalid UTF-8 in output", ErrInvalidOutput, ErrInvalidCanonicalURL)
	ErrOutputURLTooLong                error = newOutputRejectionError("URL_TOO_LONG", "crawljobsv2: output URL too long", ErrInvalidCanonicalURL)
	ErrOutputGroupIDLimit              error = newOutputRejectionError("GROUP_ID_LIMIT", "crawljobsv2: output group ID limit", ErrInvalidGroupID)
	ErrOutputContextMismatch           error = newOutputRejectionError("OUTPUT_CONTEXT_MISMATCH", "crawljobsv2: output context mismatch", ErrInvalidOutput, ErrDigestInputMismatch)

	ErrInvalidRenderPolicyArtifact = errors.New("crawljobsv2: invalid render-policy artifact")
)

// OutputRejectionError is a stable, value-redacted production output
// rejection. Instances are package-owned static sentinels so rejection classes
// cannot depend on caller-controlled input or formatted error text.
type OutputRejectionError struct {
	class         string
	message       string
	compatibility []error
}

func newOutputRejectionError(class, message string, compatibility ...error) *OutputRejectionError {
	return &OutputRejectionError{class: class, message: message, compatibility: compatibility}
}

func (err *OutputRejectionError) Error() string {
	if err == nil {
		return "crawljobsv2: output rejection"
	}
	return err.message
}

// RejectionClass returns the stable protocol-facing class for this sentinel.
func (err *OutputRejectionError) RejectionClass() string {
	if err == nil {
		return ""
	}
	return err.class
}

// Unwrap preserves errors.Is compatibility with the broader errors returned
// before output rejections acquired stable classes. A copy prevents callers
// from mutating package-owned sentinel state.
func (err *OutputRejectionError) Unwrap() []error {
	if err == nil || len(err.compatibility) == 0 {
		return nil
	}
	return append([]error(nil), err.compatibility...)
}

// OutputRejectionClass returns a class only when err contains one of the typed
// production output rejection sentinels.
func OutputRejectionClass(err error) (string, bool) {
	var rejection *OutputRejectionError
	if !errors.As(err, &rejection) || rejection.RejectionClass() == "" {
		return "", false
	}
	return rejection.RejectionClass(), true
}

const (
	lastCrawledLayout            = "Mon, 02 Jan 2006 15:04:05 UTC"
	maxRenderPolicyArtifactBytes = 1024 * 1024
)

type SourceJob struct {
	JobID        JobID
	CanonicalURL string
	ScoreText    ScoreText
	Depth        uint64
	GroupID      GroupID
	RateScopeID  RateScopeID
	Decision     PolicyDecision
}

type OutputPage struct {
	NormalizedURL      string
	HTML               []byte
	OriginalHTML       []byte
	ContentType        string
	StatusCode         uint16
	Rendered           bool
	RenderPolicyRule   string
	RenderPolicyDigest Digest
}

type OutputImage struct {
	NormalizedSourceURL string
	Alt                 string
}

type OutputDiscovery struct {
	JobID        JobID
	CanonicalURL string
	Depth        uint64
	ScoreText    ScoreText
	GroupID      GroupID
	RateScopeID  RateScopeID
	Decision     PolicyDecision
}

type outputAlias struct {
	URLID        JobID
	CanonicalURL string
	Depth        uint64
}

// SuccessfulDocumentRequest is validated evidence of one Redis-recorded
// request START from the current fence, including failed or no-network outcomes.
// The historical type name describes a successful START, not a successful fetch. It is
// retained because document output consumes these events, but robots and
// render-resource events are represented too so they cannot create invisible
// transcript gaps. Only document and redirect events derive aliases.
type SuccessfulDocumentRequest struct {
	kind               RequestKind
	target             RequestTarget
	lease              LeaseIdentity
	reservationID      ReservationID
	requestOrdinal     uint64
	redisStartedAtMS   RedisMilliseconds
	deliveryAttempts   uint64
	jobRequestStarts   uint64
	runRequestStarts   uint64
	groupRequestStarts uint64
	authority          *requestIOAuthorityState
	initialized        bool
}

// DocumentTranscript is an immutable, append-only view of every represented
// recorded request START for one lease. Errors must not be omitted from successful
// output. Per-job start snapshots are
// contiguous. Reservation ordinals are strictly increasing but may have legal
// gaps caused by reservations that never started.
type DocumentTranscript struct {
	sourceJobID        JobID
	sourceURL          string
	sourceDepth        uint64
	sourceGroupID      GroupID
	sourceRateScopeID  RateScopeID
	sourceDecision     PolicyDecision
	sourceRequestBound bool
	lease              LeaseIdentity
	runPolicy          RunPolicyAuthority
	requests           []SuccessfulDocumentRequest
	initialized        bool
}

// FinalDocumentWitness is the exact final document projection read from the
// authoritative job hash after the last request start. The terminal job-wide
// count/timestamp snapshot proves that the supplied transcript is not a stale
// prefix even when multiple starts share one Redis millisecond and the final
// redirect returns to an earlier target.
type FinalDocumentWitness struct {
	runID                      RunID
	lease                      LeaseIdentity
	redisStartedAtMS           RedisMilliseconds
	target                     RequestTarget
	targetDigest               Digest
	terminalJobRequestStarts   uint64
	leaseRequestStartsBaseline uint64
	terminalRequestStartedAtMS RedisMilliseconds
	transportSeal              *transportAuthoritySeal
	initialized                bool
}

// Keep this seal non-zero-sized for pointer identity, for the same reason as
// transportAuthoritySeal.
type renderPolicyAuthorizationSeal struct {
	marker byte
}

var decodedRenderPolicyAuthorizationSeal = renderPolicyAuthorizationSeal{marker: 1}

// RenderPolicyAuthorization binds one authenticated run and its pinned render
// digest to matcher data decoded exclusively from the exact artifact bytes.
// The matcher is immutable through the renderpolicy API and is never exposed.
type RenderPolicyAuthorization struct {
	runID              RunID
	digest             Digest
	matcher            *renderpolicy.Policy
	matcherSHA256      Digest
	runPolicyAuthority RunPolicyAuthority
	seal               *renderPolicyAuthorizationSeal
	initialized        bool
}

// OutputContext binds output to an authoritative transcript, final job witness,
// and run-pinned render policy.
type OutputContext struct {
	runID                   RunID
	jobID                   JobID
	leaseFence              Fence
	lease                   LeaseIdentity
	requestStartsBaseline   uint64
	requestStartsGeneration uint64
	// Retain exact authenticated evidence, not only the lossy last_crawled
	// display string or independently mutable source metadata. These private
	// snapshots are allocated once and shared immutably by context/chunk copies.
	sourceRequest *SuccessfulDocumentRequest
	witness       *FinalDocumentWitness
	finalTarget   RequestTarget
	lastCrawled   string
	aliases       []outputAlias
	runPolicy     RunPolicyAuthority
	renderPolicy  RenderPolicyAuthorization
	initialized   bool
}

// CrawlOutput contains only the five semantic output sections. Publication
// IDs, final keys, manifests, backlinks, and notifications intentionally have
// no field here and therefore cannot enter the output digest.
type CrawlOutput struct {
	Page        OutputPage
	Outlinks    []string
	Images      []OutputImage
	Discoveries []OutputDiscovery
}

// NewSuccessfulRequest consumes an opaque one-use I/O permit after the caller
// has resolved that exact request, including failure or no-network completion.
// Every recorded START must be consumed into the transcript regardless of outcome.
// Permit copies share the same atomic state.
func NewSuccessfulRequest(permit RequestIOPermit) (SuccessfulDocumentRequest, error) {
	if !permit.initialized || permit.authority == nil {
		return SuccessfulDocumentRequest{}, ErrInvalidResponseAuthority
	}
	binding, consumed := permit.authority.consume()
	if !consumed {
		return SuccessfulDocumentRequest{}, ErrInvalidResponseAuthority
	}
	intent := binding.intent
	reservationID, err := DeriveReservationID(binding.runPolicy, intent)
	if err != nil {
		return SuccessfulDocumentRequest{}, err
	}
	if reservationID != binding.reservationID || reservationID != binding.started.reservationID || !binding.started.ioPermission {
		return SuccessfulDocumentRequest{}, ErrDigestInputMismatch
	}
	if binding.started.startedAtMS == 0 || binding.started.jobRequestStarts == 0 {
		return SuccessfulDocumentRequest{}, ErrInvalidOutput
	}
	request := SuccessfulDocumentRequest{
		kind: intent.Decision.RequestKind, target: intent.Target, lease: intent.Lease,
		reservationID: reservationID, requestOrdinal: intent.RequestOrdinal,
		redisStartedAtMS: binding.started.startedAtMS, deliveryAttempts: binding.started.deliveryAttempts,
		jobRequestStarts: binding.started.jobRequestStarts, runRequestStarts: binding.started.runRequestStarts,
		groupRequestStarts: binding.started.groupRequestStarts, authority: permit.authority, initialized: true,
	}
	if err := validateSuccessfulDocumentRequest(request); err != nil {
		return SuccessfulDocumentRequest{}, err
	}
	return request, nil
}

// NewSuccessfulDocumentRequest is retained as the document-output-facing name.
// It consumes the same one-use permit and can represent any legal request kind.
func NewSuccessfulDocumentRequest(permit RequestIOPermit) (SuccessfulDocumentRequest, error) {
	return NewSuccessfulRequest(permit)
}

// NewRequestTranscript starts a transcript with the first represented request
// event. A fence may legally begin with robots or with its source document.
func NewRequestTranscript(runPolicy RunPolicyAuthority, source SourceJob, request SuccessfulDocumentRequest) (DocumentTranscript, error) {
	runBinding, err := runPolicy.authenticatedBinding()
	if err != nil {
		return DocumentTranscript{}, err
	}
	if err := validateSourceJobAgainstRunPolicy(runPolicy, source); err != nil {
		return DocumentTranscript{}, err
	}
	intent, err := authenticatedRequestIntent(request)
	if err != nil {
		return DocumentTranscript{}, err
	}
	requestBinding, err := validateReservationIntentAgainstRunPolicy(runPolicy, intent)
	if err != nil {
		return DocumentTranscript{}, err
	}
	if !sameRunPolicyBinding(requestBinding, runBinding) {
		return DocumentTranscript{}, ErrOutputContextMismatch
	}
	transcript := DocumentTranscript{
		sourceJobID: source.JobID, sourceURL: source.CanonicalURL, lease: request.lease,
		runPolicy: runPolicy, requests: []SuccessfulDocumentRequest{request}, initialized: true,
	}
	if request.kind == RequestDocument {
		var err error
		transcript, err = bindTranscriptSourceRequest(transcript, request)
		if err != nil {
			return DocumentTranscript{}, err
		}
	}
	if err := validateDocumentTranscript(transcript); err != nil {
		return DocumentTranscript{}, err
	}
	return transcript, nil
}

func NewDocumentTranscript(runPolicy RunPolicyAuthority, source SourceJob, request SuccessfulDocumentRequest) (DocumentTranscript, error) {
	return NewRequestTranscript(runPolicy, source, request)
}

// AppendSuccessfulRequest appends the next authenticated successful start. A
// discontinuous job-start snapshot is rejected rather than treated as an
// unauthenticated gap witness.
func (transcript DocumentTranscript) AppendSuccessfulRequest(request SuccessfulDocumentRequest) (DocumentTranscript, error) {
	if !transcript.initialized || len(transcript.requests) == 0 || len(transcript.requests) >= int(MaxRequestStartsPerRun) {
		return DocumentTranscript{}, ErrInvalidOutput
	}
	if err := validateSuccessfulDocumentRequest(request); err != nil {
		return DocumentTranscript{}, err
	}
	next := transcript
	next.requests = append([]SuccessfulDocumentRequest(nil), transcript.requests...)
	next.requests = append(next.requests, request)
	if request.kind == RequestDocument {
		var err error
		next, err = bindTranscriptSourceRequest(next, request)
		if err != nil {
			return DocumentTranscript{}, err
		}
	}
	if err := validateDocumentTranscript(next); err != nil {
		return DocumentTranscript{}, err
	}
	return next, nil
}

func (transcript DocumentTranscript) AppendRedirect(request SuccessfulDocumentRequest) (DocumentTranscript, error) {
	if request.kind != RequestRedirect {
		return DocumentTranscript{}, ErrDigestInputMismatch
	}
	return transcript.AppendSuccessfulRequest(request)
}

// parseFinalDocumentWitness is reachable only through the unexported transport
// authority; arbitrary caller arrays cannot construct production authority.
// raw is the exact ordered projection:
//
//	last_document_request_started_at_ms, last_document_request_fence,
//	last_document_target_url_id, last_document_target_url,
//	last_document_target_digest, request_starts,
//	last_request_started_at_ms, lease_request_starts_baseline, state,
//	lease_owner, lease_token, lease_fence, active_reservation_id
//
// This read authenticates a live snapshot; only BEGIN_STAGE can freeze it.
func (authority transportAuthority) parseFinalDocumentWitness(lease LeaseIdentity, raw any) (FinalDocumentWitness, error) {
	if !authority.valid() {
		return FinalDocumentWitness{}, ErrInvalidResponseAuthority
	}
	if err := validateLeaseIdentity(lease); err != nil {
		return FinalDocumentWitness{}, err
	}
	values, ok := responseArray(raw)
	if !ok || len(values) != 13 {
		return FinalDocumentWitness{}, ErrResponseArity
	}
	text := make([]string, len(values))
	for index, value := range values {
		var scalarOK bool
		text[index], scalarOK = value.(string)
		if !scalarOK || !utf8.ValidString(text[index]) {
			return FinalDocumentWitness{}, ErrResponseScalarType
		}
	}
	startedAt, err := parseResponseUint(text[0])
	if err != nil || startedAt == 0 {
		return FinalDocumentWitness{}, ErrResponseScalar
	}
	targetID, err := ParseJobID(text[2])
	if err != nil {
		return FinalDocumentWitness{}, ErrResponseScalar
	}
	target := RequestTarget{URLID: targetID, CanonicalURL: text[3]}
	targetDigest, err := DeriveTargetDigest(target)
	if err != nil || string(targetDigest) != text[4] || text[1] != canonicalDecimal(uint64(lease.Fence)) {
		return FinalDocumentWitness{}, ErrDigestInputMismatch
	}
	terminalJobRequestStarts, err := parseResponseUint(text[5])
	if err != nil || terminalJobRequestStarts == 0 || terminalJobRequestStarts > MaxRequestStartsPerRun {
		return FinalDocumentWitness{}, ErrResponseScalar
	}
	terminalRequestStartedAtMS, err := parseResponseUint(text[6])
	if err != nil || terminalRequestStartedAtMS == 0 || terminalRequestStartedAtMS < startedAt {
		return FinalDocumentWitness{}, ErrResponseScalar
	}
	baseline, err := parseResponseUint(text[7])
	if err != nil || validateRequestStartsTuple(baseline, terminalJobRequestStarts) != nil {
		return FinalDocumentWitness{}, ErrResponseScalar
	}
	if text[8] != "leased" || text[9] != string(lease.OwnerID) || text[10] != string(lease.Token) ||
		text[11] != canonicalDecimal(uint64(lease.Fence)) || text[12] != "" {
		return FinalDocumentWitness{}, ErrDigestInputMismatch
	}
	return FinalDocumentWitness{
		runID: lease.RunID, lease: lease, redisStartedAtMS: RedisMilliseconds(startedAt), target: target,
		targetDigest: targetDigest, terminalJobRequestStarts: terminalJobRequestStarts,
		leaseRequestStartsBaseline: baseline,
		terminalRequestStartedAtMS: RedisMilliseconds(terminalRequestStartedAtMS),
		transportSeal:              authority.seal, initialized: true,
	}, nil
}

// NewRenderPolicyAuthorization hashes and strictly decodes the exact policy
// artifact. Production callers cannot independently submit a loaded digest or
// an enabled-rule list.
func NewRenderPolicyAuthorization(runPolicyAuthority RunPolicyAuthority, exactPolicyArtifact []byte) (RenderPolicyAuthorization, error) {
	binding, err := runPolicyAuthority.authenticatedBinding()
	if err != nil {
		return RenderPolicyAuthorization{}, err
	}
	if len(exactPolicyArtifact) > maxRenderPolicyArtifactBytes {
		return RenderPolicyAuthorization{}, ErrInvalidRenderPolicyArtifact
	}
	artifact := append([]byte(nil), exactPolicyArtifact...)
	if plainSHA256(artifact) != binding.renderPolicySHA256 {
		return RenderPolicyAuthorization{}, ErrDigestInputMismatch
	}
	policy, err := renderpolicy.Decode(bytes.NewReader(artifact))
	if err != nil {
		return RenderPolicyAuthorization{}, ErrInvalidRenderPolicyArtifact
	}
	for _, rule := range policy.Rules() {
		if len(rule.ID) == 0 || len(rule.ID) > MaxRenderPolicyRuleIDBytes || !utf8.ValidString(rule.ID) || containsControl(rule.ID) {
			return RenderPolicyAuthorization{}, ErrInvalidRenderPolicyArtifact
		}
	}
	matcherSHA256, err := renderPolicyMatcherDigest(policy)
	if err != nil {
		return RenderPolicyAuthorization{}, ErrInvalidRenderPolicyArtifact
	}
	return RenderPolicyAuthorization{
		runID: binding.runID, digest: binding.renderPolicySHA256, matcher: policy,
		matcherSHA256: matcherSHA256, runPolicyAuthority: runPolicyAuthority,
		seal: &decodedRenderPolicyAuthorizationSeal, initialized: true,
	}, nil
}

func NewOutputContext(runPolicy RunPolicyAuthority, source SourceJob, transcript DocumentTranscript, witness FinalDocumentWitness, renderPolicy RenderPolicyAuthorization) (OutputContext, error) {
	runBinding, err := runPolicy.authenticatedBinding()
	if err != nil {
		return OutputContext{}, err
	}
	if err := validateSourceJobAgainstRunPolicy(runPolicy, source); err != nil {
		return OutputContext{}, err
	}
	if !transcript.initialized || !witness.initialized || witness.transportSeal != &redisTransportAuthoritySeal ||
		!renderPolicy.initialized || witness.lease != transcript.lease || len(transcript.requests) == 0 {
		return OutputContext{}, ErrDigestInputMismatch
	}
	if transcript.lease.RunID != runBinding.runID || witness.runID != runBinding.runID || witness.lease.RunID != runBinding.runID {
		return OutputContext{}, ErrOutputContextMismatch
	}
	if transcript.sourceJobID != source.JobID || transcript.sourceURL != source.CanonicalURL || transcript.lease.JobID != source.JobID {
		return OutputContext{}, ErrOutputContextMismatch
	}
	if err := validateDocumentTranscript(transcript); err != nil {
		return OutputContext{}, err
	}
	if err := validateTranscriptRunPolicyAuthority(transcript, runPolicy); err != nil {
		return OutputContext{}, err
	}
	sourceRequest, err := authenticatedSourceDocumentRequest(transcript)
	if err != nil {
		return OutputContext{}, err
	}
	sourceIntent, err := authenticatedRequestIntent(sourceRequest)
	if err != nil {
		return OutputContext{}, err
	}
	if err := validateSourceJobAgainstAuthenticatedRequest(source, sourceIntent); err != nil {
		return OutputContext{}, err
	}
	if err := validateRenderPolicyAuthorization(renderPolicy); err != nil {
		return OutputContext{}, err
	}
	renderBinding, err := renderPolicy.runPolicyAuthority.authenticatedBinding()
	if err != nil || renderPolicy.runID != runBinding.runID || !sameRunPolicyBinding(renderBinding, runBinding) {
		return OutputContext{}, ErrOutputContextMismatch
	}
	finalRequest, ok := finalAliasBearingRequest(transcript)
	if !ok {
		return OutputContext{}, ErrInvalidOutput
	}
	finalTargetDigest, err := DeriveTargetDigest(finalRequest.target)
	if err != nil {
		return OutputContext{}, err
	}
	if witness.redisStartedAtMS != finalRequest.redisStartedAtMS || witness.target != finalRequest.target ||
		witness.targetDigest != finalTargetDigest {
		return OutputContext{}, ErrDigestInputMismatch
	}
	terminalRequest := transcript.requests[len(transcript.requests)-1]
	if validateRequestStartsTuple(witness.leaseRequestStartsBaseline, witness.terminalJobRequestStarts) != nil ||
		!validDeliveryAttemptHistory(witness.leaseRequestStartsBaseline, witness.terminalJobRequestStarts, terminalRequest.deliveryAttempts) ||
		terminalRequest.deliveryAttempts > uint64(transcript.lease.Fence) ||
		transcript.requests[0].jobRequestStarts != witness.leaseRequestStartsBaseline+1 ||
		uint64(len(transcript.requests)) != witness.terminalJobRequestStarts-witness.leaseRequestStartsBaseline ||
		witness.terminalJobRequestStarts != terminalRequest.jobRequestStarts ||
		witness.terminalRequestStartedAtMS != terminalRequest.redisStartedAtMS {
		return OutputContext{}, ErrDigestInputMismatch
	}
	aliases, err := transcriptAliases(transcript)
	if err != nil {
		return OutputContext{}, err
	}
	return OutputContext{
		runID:                   runBinding.runID,
		jobID:                   sourceIntent.Lease.JobID,
		leaseFence:              transcript.lease.Fence,
		lease:                   transcript.lease,
		requestStartsBaseline:   witness.leaseRequestStartsBaseline,
		requestStartsGeneration: witness.terminalJobRequestStarts,
		sourceRequest:           &sourceRequest,
		witness:                 &witness,
		finalTarget:             finalRequest.target,
		lastCrawled:             formatRedisLastCrawled(finalRequest.redisStartedAtMS),
		aliases:                 aliases,
		runPolicy:               runPolicy,
		renderPolicy:            cloneRenderPolicyAuthorization(renderPolicy),
		initialized:             true,
	}, nil
}

func validateSuccessfulDocumentRequest(request SuccessfulDocumentRequest) error {
	if !request.initialized ||
		request.requestOrdinal == 0 || request.requestOrdinal > MaxReservationCreationsPerRun || request.redisStartedAtMS == 0 ||
		request.deliveryAttempts == 0 || request.deliveryAttempts > MaxDeliveryAttempts ||
		request.jobRequestStarts == 0 || request.jobRequestStarts > MaxRequestStartsPerRun ||
		request.runRequestStarts < request.jobRequestStarts || request.runRequestStarts > MaxRequestStartsPerRun ||
		request.groupRequestStarts == 0 || request.groupRequestStarts > MaxRequestStartsPerGroup {
		return ErrInvalidOutput
	}
	if err := validateLeaseIdentity(request.lease); err != nil {
		return err
	}
	if err := validateRequestTarget(request.target); err != nil {
		return err
	}
	if err := validateReservationID(request.reservationID); err != nil {
		return err
	}
	binding, ok := request.authority.consumedBinding()
	if !ok || binding.intent.Decision.RequestKind != request.kind || binding.intent.Target != request.target ||
		binding.intent.Lease != request.lease || binding.intent.RequestOrdinal != request.requestOrdinal ||
		binding.reservationID != request.reservationID || binding.started.reservationID != request.reservationID ||
		binding.started.startedAtMS != request.redisStartedAtMS || binding.started.deliveryAttempts != request.deliveryAttempts ||
		binding.started.jobRequestStarts != request.jobRequestStarts || binding.started.runRequestStarts != request.runRequestStarts ||
		binding.started.groupRequestStarts != request.groupRequestStarts || !binding.started.ioPermission {
		return ErrInvalidResponseAuthority
	}
	if _, err := validateReservationIntentAgainstRunPolicy(binding.runPolicy, binding.intent); err != nil {
		return err
	}
	return nil
}

// bindTranscriptSourceRequest snapshots source semantics only from the
// transport-authenticated document start. The caller-supplied SourceJob is not
// permitted to select alias depth or policy lineage.
func bindTranscriptSourceRequest(transcript DocumentTranscript, request SuccessfulDocumentRequest) (DocumentTranscript, error) {
	intent, err := authenticatedRequestIntent(request)
	if err != nil {
		return DocumentTranscript{}, err
	}
	if request.kind != RequestDocument || intent.Target.URLID != transcript.sourceJobID ||
		intent.Target.CanonicalURL != transcript.sourceURL || intent.Lease != transcript.lease {
		return DocumentTranscript{}, ErrOutputContextMismatch
	}
	decision := intent.Decision
	if transcript.sourceRequestBound {
		if transcript.sourceDepth != decision.Depth || transcript.sourceGroupID != decision.GroupID ||
			transcript.sourceRateScopeID != decision.RateScopeID || transcript.sourceDecision != decision {
			return DocumentTranscript{}, ErrOutputContextMismatch
		}
		return transcript, nil
	}
	transcript.sourceDepth = decision.Depth
	transcript.sourceGroupID = decision.GroupID
	transcript.sourceRateScopeID = decision.RateScopeID
	transcript.sourceDecision = decision
	transcript.sourceRequestBound = true
	return transcript, nil
}

func authenticatedRequestIntent(request SuccessfulDocumentRequest) (ReservationIntent, error) {
	if err := validateSuccessfulDocumentRequest(request); err != nil {
		return ReservationIntent{}, err
	}
	binding, ok := request.authority.consumedBinding()
	if !ok {
		return ReservationIntent{}, ErrInvalidResponseAuthority
	}
	return binding.intent, nil
}

func authenticatedSourceDocumentIntent(transcript DocumentTranscript) (ReservationIntent, error) {
	request, err := authenticatedSourceDocumentRequest(transcript)
	if err != nil {
		return ReservationIntent{}, err
	}
	return authenticatedRequestIntent(request)
}

func authenticatedSourceDocumentRequest(transcript DocumentTranscript) (SuccessfulDocumentRequest, error) {
	for _, request := range transcript.requests {
		if request.kind != RequestDocument {
			continue
		}
		if err := validateSuccessfulDocumentRequest(request); err != nil {
			return SuccessfulDocumentRequest{}, err
		}
		return request, nil
	}
	return SuccessfulDocumentRequest{}, ErrOutputContextMismatch
}

func validateSourceJobAgainstAuthenticatedRequest(source SourceJob, intent ReservationIntent) error {
	sourceTarget := RequestTarget{URLID: source.JobID, CanonicalURL: source.CanonicalURL}
	decision := intent.Decision
	if intent.Lease.JobID != source.JobID || intent.Target != sourceTarget || decision.RequestKind != RequestDocument ||
		source.Depth != decision.Depth || source.GroupID != decision.GroupID || source.RateScopeID != decision.RateScopeID ||
		source.Decision != decision {
		return ErrOutputContextMismatch
	}
	return nil
}

func validateTranscriptRunPolicyAuthority(transcript DocumentTranscript, runPolicy RunPolicyAuthority) error {
	binding, err := runPolicy.authenticatedBinding()
	if err != nil {
		return err
	}
	transcriptBinding, err := transcript.runPolicy.authenticatedBinding()
	if err != nil || !sameRunPolicyBinding(transcriptBinding, binding) || transcript.lease.RunID != binding.runID {
		return ErrOutputContextMismatch
	}
	for _, request := range transcript.requests {
		intent, err := authenticatedRequestIntent(request)
		if err != nil {
			return err
		}
		requestBinding, err := validateReservationIntentAgainstRunPolicy(runPolicy, intent)
		if err != nil {
			return err
		}
		if !sameRunPolicyBinding(requestBinding, binding) || intent.Lease.RunID != binding.runID || intent.CrawlPolicyDigest != binding.crawlPolicySHA256 {
			return ErrOutputContextMismatch
		}
	}
	return nil
}

func validateDocumentTranscript(transcript DocumentTranscript) error {
	if !transcript.initialized || len(transcript.requests) == 0 || len(transcript.requests) > int(MaxRequestStartsPerRun) {
		return ErrInvalidOutput
	}
	if err := validateJobURL(transcript.sourceJobID, transcript.sourceURL); err != nil {
		return err
	}
	if err := validateLeaseIdentity(transcript.lease); err != nil || transcript.lease.JobID != transcript.sourceJobID {
		return ErrDigestInputMismatch
	}
	if err := validateTranscriptRunPolicyAuthority(transcript, transcript.runPolicy); err != nil {
		return err
	}

	hasDocument := false
	preDocumentRobots := 0
	var sourceIntent ReservationIntent
	var initialRobotsIntent ReservationIntent
	groupSnapshots := make(map[GroupID]uint64)
	for index, request := range transcript.requests {
		if err := validateSuccessfulDocumentRequest(request); err != nil {
			return err
		}
		if request.lease != transcript.lease {
			return ErrDigestInputMismatch
		}
		if index == 0 {
			if request.kind != RequestRobots && request.kind != RequestDocument {
				return ErrDigestInputMismatch
			}
		} else {
			previous := transcript.requests[index-1]
			if previous.requestOrdinal >= request.requestOrdinal ||
				previous.jobRequestStarts == MaxExactInteger || request.jobRequestStarts != previous.jobRequestStarts+1 ||
				request.redisStartedAtMS < previous.redisStartedAtMS ||
				request.deliveryAttempts != previous.deliveryAttempts ||
				request.runRequestStarts <= previous.runRequestStarts {
				return ErrDigestInputMismatch
			}
		}

		groupID := request.authority.binding.intent.Decision.GroupID
		if previous, seen := groupSnapshots[groupID]; seen && request.groupRequestStarts <= previous {
			return ErrDigestInputMismatch
		}
		groupSnapshots[groupID] = request.groupRequestStarts

		switch request.kind {
		case RequestRobots:
			if !hasDocument {
				preDocumentRobots++
				if preDocumentRobots > 1 {
					return ErrOutputContextMismatch
				}
				robotsIntent, intentErr := authenticatedRequestIntent(request)
				if intentErr != nil {
					return intentErr
				}
				initialRobotsIntent = robotsIntent
			}
		case RequestDocument:
			if hasDocument || request.target.URLID != transcript.sourceJobID || request.target.CanonicalURL != transcript.sourceURL {
				return ErrDigestInputMismatch
			}
			documentIntent, intentErr := authenticatedRequestIntent(request)
			if intentErr != nil {
				return intentErr
			}
			sourceIntent = documentIntent
			hasDocument = true
		case RequestRedirect:
			if !hasDocument {
				return ErrDigestInputMismatch
			}
		case RequestRenderResource:
			if !hasDocument {
				return ErrDigestInputMismatch
			}
		default:
			return ErrInvalidRequestKind
		}
	}
	if !hasDocument {
		if transcript.sourceRequestBound {
			return ErrOutputContextMismatch
		}
		return nil
	}
	decision := sourceIntent.Decision
	if !transcript.sourceRequestBound || sourceIntent.Target.URLID != transcript.sourceJobID ||
		sourceIntent.Target.CanonicalURL != transcript.sourceURL || transcript.sourceDepth != decision.Depth ||
		transcript.sourceGroupID != decision.GroupID || transcript.sourceRateScopeID != decision.RateScopeID ||
		transcript.sourceDecision != decision {
		return ErrOutputContextMismatch
	}
	if preDocumentRobots == 1 {
		robotsDecision := initialRobotsIntent.Decision
		if initialRobotsIntent.CrawlPolicyDigest != sourceIntent.CrawlPolicyDigest ||
			robotsDecision.Depth != decision.Depth || robotsDecision.GroupID != decision.GroupID ||
			robotsDecision.RateScopeID != decision.RateScopeID {
			return ErrOutputContextMismatch
		}
	}
	_, err := transcriptAliases(transcript)
	return err
}

func finalAliasBearingRequest(transcript DocumentTranscript) (SuccessfulDocumentRequest, bool) {
	for index := len(transcript.requests) - 1; index >= 0; index-- {
		request := transcript.requests[index]
		if request.kind == RequestDocument || request.kind == RequestRedirect {
			return request, true
		}
	}
	return SuccessfulDocumentRequest{}, false
}

func validateRenderPolicyAuthorization(authorization RenderPolicyAuthorization) error {
	if !authorization.initialized || authorization.seal != &decodedRenderPolicyAuthorizationSeal || authorization.matcher == nil {
		return ErrInvalidRenderPolicyArtifact
	}
	binding, err := authorization.runPolicyAuthority.authenticatedBinding()
	if err != nil {
		return err
	}
	if authorization.runID != binding.runID || authorization.digest != binding.renderPolicySHA256 {
		return ErrInvalidRenderPolicyArtifact
	}
	for _, rule := range authorization.matcher.Rules() {
		if len(rule.ID) == 0 || len(rule.ID) > MaxRenderPolicyRuleIDBytes || !utf8.ValidString(rule.ID) || containsControl(rule.ID) {
			return ErrInvalidRenderPolicyArtifact
		}
	}
	matcherSHA256, err := renderPolicyMatcherDigest(authorization.matcher)
	if err != nil || matcherSHA256 != authorization.matcherSHA256 {
		return ErrInvalidRenderPolicyArtifact
	}
	return nil
}

func cloneRenderPolicyAuthorization(authorization RenderPolicyAuthorization) RenderPolicyAuthorization {
	return authorization
}

func renderPolicyMatcherDigest(policy *renderpolicy.Policy) (Digest, error) {
	if policy == nil {
		return "", ErrInvalidRenderPolicyArtifact
	}
	encoded, err := json.Marshal(policy.Rules())
	if err != nil {
		return "", ErrInvalidRenderPolicyArtifact
	}
	return plainSHA256(encoded), nil
}

func (authorization RenderPolicyAuthorization) match(canonicalURL string) (renderpolicy.Rule, error) {
	if err := validateRenderPolicyAuthorization(authorization); err != nil {
		return renderpolicy.Rule{}, ErrInvalidRenderPolicyArtifact
	}
	rule, err := authorization.matcher.Match(canonicalURL)
	if err != nil || !rule.Enabled || rule.ID == "" {
		return renderpolicy.Rule{}, ErrOutputInvalidRenderRelation
	}
	return rule, nil
}

func validateOutputContextAuthority(context OutputContext) error {
	if !context.initialized {
		return ErrOutputContextMismatch
	}
	binding, err := context.runPolicy.authenticatedBinding()
	if err != nil {
		return err
	}
	if context.runID != binding.runID || validateJobID(context.jobID) != nil || validateRequestTarget(context.finalTarget) != nil {
		return ErrOutputContextMismatch
	}
	if validateLeaseIdentity(context.lease) != nil || context.lease.RunID != context.runID ||
		context.lease.JobID != context.jobID || context.lease.Fence != context.leaseFence ||
		validateRequestStartsTuple(context.requestStartsBaseline, context.requestStartsGeneration) != nil {
		return ErrOutputContextMismatch
	}
	if context.sourceRequest == nil || context.witness == nil {
		return ErrOutputContextMismatch
	}
	sourceIntent, err := authenticatedRequestIntent(*context.sourceRequest)
	if err != nil {
		return err
	}
	sourceBinding, err := validateReservationIntentAgainstRunPolicy(context.runPolicy, sourceIntent)
	if err != nil || !sameRunPolicyBinding(sourceBinding, binding) || sourceIntent.Lease != context.lease ||
		sourceIntent.Decision.RequestKind != RequestDocument || sourceIntent.Target.URLID != context.jobID ||
		context.sourceRequest.deliveryAttempts > uint64(context.lease.Fence) ||
		!validDeliveryAttemptHistory(context.requestStartsBaseline, context.requestStartsGeneration, context.sourceRequest.deliveryAttempts) {
		return ErrOutputContextMismatch
	}
	witness := context.witness
	targetDigest, err := DeriveTargetDigest(context.finalTarget)
	if err != nil || !witness.initialized || witness.transportSeal != &redisTransportAuthoritySeal ||
		witness.runID != context.runID || witness.lease != context.lease || witness.target != context.finalTarget || witness.targetDigest != targetDigest ||
		witness.leaseRequestStartsBaseline != context.requestStartsBaseline || witness.terminalJobRequestStarts != context.requestStartsGeneration ||
		witness.redisStartedAtMS == 0 || witness.terminalRequestStartedAtMS < witness.redisStartedAtMS ||
		context.lastCrawled != formatRedisLastCrawled(witness.redisStartedAtMS) {
		return ErrOutputContextMismatch
	}
	for _, alias := range context.aliases {
		if alias.Depth != sourceIntent.Decision.Depth {
			return ErrOutputContextMismatch
		}
	}
	if err := validateRenderPolicyAuthorization(context.renderPolicy); err != nil {
		return err
	}
	renderBinding, err := context.renderPolicy.runPolicyAuthority.authenticatedBinding()
	if err != nil || context.renderPolicy.runID != context.runID || !sameRunPolicyBinding(renderBinding, binding) {
		return ErrOutputContextMismatch
	}
	return nil
}

func transcriptAliases(transcript DocumentTranscript) ([]outputAlias, error) {
	if !transcript.sourceRequestBound {
		return nil, ErrOutputContextMismatch
	}
	aliasesByID := make(map[JobID]outputAlias, len(transcript.requests))
	for _, request := range transcript.requests {
		if request.kind != RequestDocument && request.kind != RequestRedirect {
			continue
		}
		if existing, ok := aliasesByID[request.target.URLID]; ok && existing.CanonicalURL != request.target.CanonicalURL {
			return nil, ErrURLIdentityMismatch
		}
		aliasesByID[request.target.URLID] = outputAlias{
			URLID: request.target.URLID, CanonicalURL: request.target.CanonicalURL, Depth: transcript.sourceDepth,
		}
	}
	if len(aliasesByID) == 0 || len(aliasesByID) > MaxAliasesPerJob {
		return nil, ErrOutputAliasCountLimit
	}
	aliases := make([]outputAlias, 0, len(aliasesByID))
	for _, alias := range aliasesByID {
		aliases = append(aliases, alias)
	}
	sort.Slice(aliases, func(left, right int) bool { return string(aliases[left].URLID) < string(aliases[right].URLID) })
	return aliases, nil
}

// DeriveSourceDigest is the non-authoritative pre-run source formula. It
// validates each source structurally; run-consuming enqueue and audit
// boundaries additionally require RunPolicyAuthority.
func DeriveSourceDigest(jobs []SourceJob) (Digest, error) {
	if len(jobs) > MaxJobsPerRun {
		return "", ErrInputLimitExceeded
	}
	ordered := append([]SourceJob(nil), jobs...)
	sort.Slice(ordered, func(left, right int) bool {
		return string(ordered[left].JobID) < string(ordered[right].JobID)
	})

	records := make([]Record, 0, len(ordered))
	for index, job := range ordered {
		if index > 0 && job.JobID == ordered[index-1].JobID {
			return "", ErrDuplicateSourceJob
		}
		record, err := sourceJobRecord(job)
		if err != nil {
			return "", err
		}
		records = append(records, record)
	}
	section, err := EncodeSection("jobs", records)
	if err != nil {
		return "", err
	}
	return digestEncoded("mifolyo:crawl-source:v2", section), nil
}

func DeriveOutputDigest(context OutputContext, output CrawlOutput) (Digest, error) {
	digest, _, err := deriveOutputDigestAndCounts(context, output)
	return digest, err
}

// Counts come from the same validated canonical records that enter the digest:
// final page fields (including publication_id), outlinks, discoveries, aliases,
// images. In particular, aliases are deduplicated transcript semantics, not the
// number of request events. Duplicate caller output records remain errors.
func deriveOutputDigestAndCounts(context OutputContext, output CrawlOutput) (Digest, [5]uint64, error) {
	if err := validateOutputContextAuthority(context); err != nil {
		return "", [5]uint64{}, err
	}
	if err := validateOutputDiscoveriesAgainstRunPolicy(context.runPolicy, output.Discoveries); err != nil {
		return "", [5]uint64{}, err
	}
	pageRecord, err := outputPageRecord(context, output.Page)
	if err != nil {
		return "", [5]uint64{}, err
	}
	outlinkRecords, err := outputOutlinkRecords(output.Page.NormalizedURL, output.Outlinks)
	if err != nil {
		return "", [5]uint64{}, err
	}
	imageRecords, err := outputImageRecords(output.Images)
	if err != nil {
		return "", [5]uint64{}, err
	}
	discoveryRecords, err := outputDiscoveryRecords(output.Discoveries)
	if err != nil {
		return "", [5]uint64{}, err
	}
	aliasRecords, err := outputAliasRecords(context.aliases)
	if err != nil {
		return "", [5]uint64{}, err
	}

	pageSection, err := EncodeSection("page", []Record{pageRecord})
	if err != nil {
		return "", [5]uint64{}, err
	}
	outlinksSection, err := EncodeSection("outlinks", outlinkRecords)
	if err != nil {
		return "", [5]uint64{}, err
	}
	imagesSection, err := EncodeSection("images", imageRecords)
	if err != nil {
		return "", [5]uint64{}, err
	}
	discoveriesSection, err := EncodeSection("discoveries", discoveryRecords)
	if err != nil {
		return "", [5]uint64{}, err
	}
	aliasesSection, err := EncodeSection("aliases", aliasRecords)
	if err != nil {
		return "", [5]uint64{}, err
	}
	return digestEncoded(
		"mifolyo:crawl-output:v2",
		pageSection,
		outlinksSection,
		imagesSection,
		discoveriesSection,
		aliasesSection,
	), [5]uint64{uint64(len(pageRecord) + 1), uint64(len(outlinkRecords)), uint64(len(discoveryRecords)), uint64(len(aliasRecords)), uint64(len(imageRecords))}, nil
}

func sourceJobRecord(job SourceJob) (Record, error) {
	if err := validateJobURL(job.JobID, job.CanonicalURL); err != nil {
		return nil, err
	}
	if err := validateScoreText(job.ScoreText); err != nil {
		return nil, err
	}
	if err := validateNonnegativeExactInteger(job.Depth); err != nil {
		return nil, err
	}
	if err := validateGroupID(job.GroupID); err != nil {
		return nil, err
	}
	if err := validateRateScopeID(job.RateScopeID); err != nil {
		return nil, err
	}
	decisionDigest, err := validateDocumentPolicyBinding(job.JobID, job.CanonicalURL, job.Depth, job.GroupID, job.RateScopeID, job.Decision)
	if err != nil {
		return nil, err
	}
	return Record{
		textField("job_id", string(job.JobID)),
		textField("canonical_url", job.CanonicalURL),
		textField("score_text", string(job.ScoreText)),
		textField("depth", canonicalDecimal(job.Depth)),
		textField("group_id", string(job.GroupID)),
		textField("rate_scope_id", string(job.RateScopeID)),
		textField("policy_decision_sha256", string(decisionDigest)),
	}, nil
}

func completeSourceJobRecord(job SourceJob) (Record, error) {
	if _, err := sourceJobRecord(job); err != nil {
		return nil, err
	}
	decisionDigest, err := validateDocumentPolicyBinding(job.JobID, job.CanonicalURL, job.Depth, job.GroupID, job.RateScopeID, job.Decision)
	if err != nil {
		return nil, err
	}
	groupScopeID, err := DeriveGroupScopeID(job.RateScopeID)
	if err != nil {
		return nil, err
	}
	origin, err := DeriveCanonicalOrigin(job.CanonicalURL)
	if err != nil {
		return nil, err
	}
	originScopeID, err := DeriveOriginScopeID(origin)
	if err != nil {
		return nil, err
	}
	return Record{
		textField("job_id", string(job.JobID)),
		textField("canonical_url", job.CanonicalURL),
		textField("score_text", string(job.ScoreText)),
		textField("depth", canonicalDecimal(job.Depth)),
		textField("group_id", string(job.GroupID)),
		textField("rate_scope_id", string(job.RateScopeID)),
		textField("group_scope_id", string(groupScopeID)),
		textField("initial_origin_scope_id", string(originScopeID)),
		textField("policy_decision_sha256", string(decisionDigest)),
	}, nil
}

func outputPageRecord(context OutputContext, page OutputPage) (Record, error) {
	if err := validateOutputContextAuthority(context); err != nil {
		return nil, err
	}
	if err := validateOutputCanonicalURL(page.NormalizedURL); err != nil {
		return nil, err
	}
	if page.NormalizedURL != context.finalTarget.CanonicalURL {
		return nil, ErrOutputContextMismatch
	}
	if !utf8.Valid(page.HTML) || !utf8.Valid(page.OriginalHTML) {
		return nil, ErrOutputInvalidUTF8
	}
	if len(page.HTML) > MaxPageBlobBytes || len(page.OriginalHTML) > MaxPageBlobBytes {
		return nil, ErrOutputPageBlobLimit
	}
	if len(page.HTML)+len(page.OriginalHTML) > MaxCombinedHTMLBytes {
		return nil, ErrOutputCombinedHTMLLimit
	}
	if err := validateContentType(page.ContentType); err != nil {
		return nil, err
	}
	if page.StatusCode < 100 || page.StatusCode > 399 {
		return nil, ErrOutputStatusCodeRange
	}
	if page.Rendered {
		if !utf8.ValidString(page.RenderPolicyRule) {
			return nil, ErrOutputInvalidUTF8
		}
		if len(page.RenderPolicyRule) > MaxRenderPolicyRuleIDBytes {
			return nil, ErrOutputRenderRuleLimit
		}
		if len(page.OriginalHTML) == 0 || len(page.RenderPolicyRule) == 0 || containsControl(page.RenderPolicyRule) {
			return nil, ErrOutputInvalidRenderRelation
		}
		if err := validateNonzeroDigest(page.RenderPolicyDigest); err != nil {
			return nil, ErrOutputInvalidRenderPolicyDigest
		}
		if page.RenderPolicyDigest != context.renderPolicy.digest {
			return nil, ErrDigestInputMismatch
		}
		matchedRule, err := context.renderPolicy.match(context.finalTarget.CanonicalURL)
		if err != nil || !matchedRule.Enabled || matchedRule.ID != page.RenderPolicyRule {
			return nil, ErrOutputInvalidRenderRelation
		}
	} else if len(page.OriginalHTML) != 0 || page.RenderPolicyRule != "" || page.RenderPolicyDigest != "" {
		return nil, ErrOutputInvalidRenderRelation
	}

	rendered := "false"
	if page.Rendered {
		rendered = "true"
	}
	return Record{
		textField("normalized_url", page.NormalizedURL),
		{Name: "html", Value: append([]byte(nil), page.HTML...)},
		{Name: "original_html", Value: append([]byte(nil), page.OriginalHTML...)},
		textField("content_type", page.ContentType),
		textField("status_code", fmt.Sprintf("%03d", page.StatusCode)),
		textField("last_crawled", context.lastCrawled),
		textField("rendered", rendered),
		textField("render_policy_rule", page.RenderPolicyRule),
		textField("render_policy_sha256", string(page.RenderPolicyDigest)),
	}, nil
}

func outputOutlinkRecords(pageURL string, outlinks []string) ([]Record, error) {
	if len(outlinks) > MaxOutlinksPerJob {
		return nil, ErrOutputCountLimit
	}
	ordered := append([]string(nil), outlinks...)
	sort.Strings(ordered)
	records := make([]Record, 0, len(ordered))
	for index, targetURL := range ordered {
		if err := validateOutputCanonicalURL(targetURL); err != nil {
			return nil, err
		}
		if targetURL == pageURL {
			return nil, ErrInvalidOutput
		}
		if index > 0 && targetURL == ordered[index-1] {
			return nil, ErrDuplicateOutlink
		}
		records = append(records, Record{textField("target_url", targetURL)})
	}
	return records, nil
}

func outputImageRecords(images []OutputImage) ([]Record, error) {
	if len(images) > MaxImagesPerPage {
		return nil, ErrOutputCountLimit
	}
	ordered := append([]OutputImage(nil), images...)
	sort.Slice(ordered, func(left, right int) bool {
		return ordered[left].NormalizedSourceURL < ordered[right].NormalizedSourceURL
	})
	records := make([]Record, 0, len(ordered))
	for index, image := range ordered {
		if err := validateOutputCanonicalURL(image.NormalizedSourceURL); err != nil {
			return nil, err
		}
		if !utf8.ValidString(image.Alt) {
			return nil, ErrOutputInvalidUTF8
		}
		if len(image.Alt) > MaxImageAltBytes {
			return nil, ErrOutputImageAltLimit
		}
		if index > 0 && image.NormalizedSourceURL == ordered[index-1].NormalizedSourceURL {
			return nil, ErrDuplicateImage
		}
		records = append(records, Record{
			textField("normalized_source_url", image.NormalizedSourceURL),
			textField("alt", image.Alt),
		})
	}
	return records, nil
}

func outputDiscoveryRecords(discoveries []OutputDiscovery) ([]Record, error) {
	if len(discoveries) > MaxDiscoveriesPerJob {
		return nil, ErrOutputCountLimit
	}
	ordered := append([]OutputDiscovery(nil), discoveries...)
	sort.Slice(ordered, func(left, right int) bool {
		return string(ordered[left].JobID) < string(ordered[right].JobID)
	})
	records := make([]Record, 0, len(ordered))
	for index, discovery := range ordered {
		if index > 0 && discovery.JobID == ordered[index-1].JobID {
			return nil, ErrDuplicateDiscovery
		}
		if err := validateJobURL(discovery.JobID, discovery.CanonicalURL); err != nil {
			return nil, err
		}
		if err := validateNonnegativeExactInteger(discovery.Depth); err != nil {
			return nil, err
		}
		if err := validateScoreText(discovery.ScoreText); err != nil {
			return nil, err
		}
		if len(discovery.GroupID) > MaxPolicyGroupIDBytes {
			return nil, ErrOutputGroupIDLimit
		}
		if err := validateGroupID(discovery.GroupID); err != nil {
			return nil, err
		}
		if err := validateRateScopeID(discovery.RateScopeID); err != nil {
			return nil, err
		}
		decisionDigest, err := validateDocumentPolicyBinding(
			discovery.JobID,
			discovery.CanonicalURL,
			discovery.Depth,
			discovery.GroupID,
			discovery.RateScopeID,
			discovery.Decision,
		)
		if err != nil {
			return nil, err
		}
		records = append(records, Record{
			textField("job_id", string(discovery.JobID)),
			textField("canonical_url", discovery.CanonicalURL),
			textField("depth", canonicalDecimal(discovery.Depth)),
			textField("score_text", string(discovery.ScoreText)),
			textField("group_id", string(discovery.GroupID)),
			textField("rate_scope_id", string(discovery.RateScopeID)),
			textField("policy_decision_sha256", string(decisionDigest)),
		})
	}
	return records, nil
}

func completeDiscoveryRecord(discovery OutputDiscovery) (Record, error) {
	if len(discovery.GroupID) > MaxPolicyGroupIDBytes {
		return nil, ErrOutputGroupIDLimit
	}
	decisionDigest, err := validateDocumentPolicyBinding(
		discovery.JobID,
		discovery.CanonicalURL,
		discovery.Depth,
		discovery.GroupID,
		discovery.RateScopeID,
		discovery.Decision,
	)
	if err != nil {
		return nil, err
	}
	if err := validateScoreText(discovery.ScoreText); err != nil {
		return nil, err
	}
	groupScopeID, err := DeriveGroupScopeID(discovery.RateScopeID)
	if err != nil {
		return nil, err
	}
	origin, err := DeriveCanonicalOrigin(discovery.CanonicalURL)
	if err != nil {
		return nil, err
	}
	originScopeID, err := DeriveOriginScopeID(origin)
	if err != nil {
		return nil, err
	}
	return Record{
		textField("job_id", string(discovery.JobID)),
		textField("canonical_url", discovery.CanonicalURL),
		textField("depth", canonicalDecimal(discovery.Depth)),
		textField("score_text", string(discovery.ScoreText)),
		textField("group_id", string(discovery.GroupID)),
		textField("rate_scope_id", string(discovery.RateScopeID)),
		textField("group_scope_id", string(groupScopeID)),
		textField("initial_origin_scope_id", string(originScopeID)),
		textField("policy_decision_sha256", string(decisionDigest)),
	}, nil
}

func outputAliasRecords(aliases []outputAlias) ([]Record, error) {
	if len(aliases) == 0 || len(aliases) > MaxAliasesPerJob {
		return nil, ErrOutputAliasCountLimit
	}
	ordered := append([]outputAlias(nil), aliases...)
	sort.Slice(ordered, func(left, right int) bool {
		return string(ordered[left].URLID) < string(ordered[right].URLID)
	})
	records := make([]Record, 0, len(ordered))
	depth := ordered[0].Depth
	for index, alias := range ordered {
		if index > 0 && alias.URLID == ordered[index-1].URLID {
			return nil, ErrDuplicateAlias
		}
		if err := validateJobURL(alias.URLID, alias.CanonicalURL); err != nil {
			return nil, err
		}
		if err := validateNonnegativeExactInteger(alias.Depth); err != nil {
			return nil, err
		}
		if alias.Depth != depth {
			return nil, ErrInvalidOutput
		}
		records = append(records, Record{
			textField("url_id", string(alias.URLID)),
			textField("canonical_url", alias.CanonicalURL),
			textField("depth", canonicalDecimal(alias.Depth)),
		})
	}
	return records, nil
}

func validateDocumentPolicyBinding(jobID JobID, canonicalURL string, depth uint64, groupID GroupID, rateScopeID RateScopeID, decision PolicyDecision) (Digest, error) {
	if err := validateJobURL(jobID, canonicalURL); err != nil {
		return "", err
	}
	targetDigest, err := DeriveTargetDigest(RequestTarget{URLID: jobID, CanonicalURL: canonicalURL})
	if err != nil {
		return "", err
	}
	groupScopeID, err := DeriveGroupScopeID(rateScopeID)
	if err != nil {
		return "", err
	}
	origin, err := DeriveCanonicalOrigin(canonicalURL)
	if err != nil {
		return "", err
	}
	originScopeID, err := DeriveOriginScopeID(origin)
	if err != nil {
		return "", err
	}
	if decision.RequestKind != RequestDocument ||
		decision.TargetURLID != jobID ||
		decision.TargetDigest != targetDigest ||
		decision.Depth != depth ||
		decision.GroupID != groupID ||
		decision.RateScopeID != rateScopeID ||
		decision.GroupScopeID != groupScopeID ||
		decision.OriginScopeID != originScopeID {
		return "", ErrDigestInputMismatch
	}
	return DerivePolicyDecisionDigest(decision)
}

func formatRedisLastCrawled(milliseconds RedisMilliseconds) string {
	seconds := int64(uint64(milliseconds) / 1000)
	return time.Unix(seconds, 0).UTC().Format(lastCrawledLayout)
}

func validateJobURL(jobID JobID, canonicalURL string) error {
	if err := validateJobID(jobID); err != nil {
		return err
	}
	identity, err := requireCanonicalURL(canonicalURL)
	if err != nil {
		return err
	}
	if identity.URLID != string(jobID) {
		return ErrURLIdentityMismatch
	}
	return nil
}

func validateContentType(value string) error {
	if !utf8.ValidString(value) {
		return ErrOutputInvalidUTF8
	}
	if len(value) > 1024 {
		return ErrOutputContentTypeLimit
	}
	if value == "" || strings.TrimSpace(value) != value {
		return ErrOutputInvalidContentType
	}
	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil || !strings.EqualFold(mediaType, "text/html") || len(parameters) > 1 {
		return ErrOutputInvalidContentType
	}
	if len(parameters) == 1 {
		charset, ok := parameters["charset"]
		if !ok || !strings.EqualFold(charset, "utf-8") {
			return ErrOutputInvalidContentType
		}
	}
	return nil
}

func validateOutputCanonicalURL(value string) error {
	if !utf8.ValidString(value) {
		return ErrOutputInvalidUTF8
	}
	if len(value) > MaxCanonicalURLBytes {
		return ErrOutputURLTooLong
	}
	_, err := requireCanonicalURL(value)
	return err
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}
