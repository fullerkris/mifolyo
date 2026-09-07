package crawljobsv2

import (
	"errors"
	"fmt"
	"mime"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrDuplicateSourceJob = errors.New("crawljobsv2: duplicate source job")
	ErrDuplicateOutlink   = errors.New("crawljobsv2: duplicate outlink")
	ErrDuplicateImage     = errors.New("crawljobsv2: duplicate image")
	ErrDuplicateDiscovery = errors.New("crawljobsv2: duplicate discovery")
	ErrDuplicateAlias     = errors.New("crawljobsv2: duplicate alias")
	ErrInvalidOutput      = errors.New("crawljobsv2: invalid output")
)

const lastCrawledLayout = "Mon, 02 Jan 2006 15:04:05 UTC"

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
// document or redirect start from the current fence. Its fields are private so
// arbitrary client wall time or an unrelated target cannot enter output
// identity construction.
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
	initialized        bool
}

// DocumentTranscript is an immutable, append-only view of the successful
// document/redirect requests for one lease. Redirect ordinals and per-job start
// snapshots must be contiguous, preventing omitted or reordered hops.
type DocumentTranscript struct {
	sourceJobID JobID
	sourceURL   string
	lease       LeaseIdentity
	requests    []SuccessfulDocumentRequest
	initialized bool
}

// FinalDocumentWitness is the exact final document projection read from the
// authoritative job hash after the last request start.
type FinalDocumentWitness struct {
	lease            LeaseIdentity
	redisStartedAtMS RedisMilliseconds
	target           RequestTarget
	targetDigest     Digest
	initialized      bool
}

// RenderPolicyAuthorization binds the run-pinned policy digest to the digest of
// the policy actually loaded by the caller and its enabled rule IDs.
type RenderPolicyAuthorization struct {
	digest       Digest
	enabledRules map[string]struct{}
	initialized  bool
}

// OutputContext binds output to an authoritative transcript, final job witness,
// and run-pinned render policy.
type OutputContext struct {
	jobID        JobID
	leaseFence   Fence
	finalTarget  RequestTarget
	lastCrawled  string
	aliases      []outputAlias
	renderPolicy RenderPolicyAuthorization
	initialized  bool
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

// NewSuccessfulDocumentRequest converts an opaque I/O permit into successful
// document-chain evidence after the caller has completed that request.
func NewSuccessfulDocumentRequest(permit RequestIOPermit) (SuccessfulDocumentRequest, error) {
	if !permit.initialized || !permit.started.ioPermission {
		return SuccessfulDocumentRequest{}, ErrInvalidResponseAuthority
	}
	intent := permit.intent
	reservationID, err := DeriveReservationID(intent)
	if err != nil || reservationID != permit.reservationID || reservationID != permit.started.reservationID {
		return SuccessfulDocumentRequest{}, ErrDigestInputMismatch
	}
	if intent.Decision.RequestKind != RequestDocument && intent.Decision.RequestKind != RequestRedirect {
		return SuccessfulDocumentRequest{}, ErrInvalidOutput
	}
	if permit.started.startedAtMS == 0 || permit.started.jobRequestStarts == 0 {
		return SuccessfulDocumentRequest{}, ErrInvalidOutput
	}
	return SuccessfulDocumentRequest{
		kind: intent.Decision.RequestKind, target: intent.Target, lease: intent.Lease,
		reservationID: reservationID, requestOrdinal: intent.RequestOrdinal,
		redisStartedAtMS: permit.started.startedAtMS, deliveryAttempts: permit.started.deliveryAttempts,
		jobRequestStarts: permit.started.jobRequestStarts, runRequestStarts: permit.started.runRequestStarts,
		groupRequestStarts: permit.started.groupRequestStarts, initialized: true,
	}, nil
}

func NewDocumentTranscript(source SourceJob, request SuccessfulDocumentRequest) (DocumentTranscript, error) {
	if _, err := sourceJobRecord(source); err != nil {
		return DocumentTranscript{}, err
	}
	if err := validateSuccessfulDocumentRequest(request); err != nil {
		return DocumentTranscript{}, err
	}
	if request.lease.JobID != source.JobID || request.kind != RequestDocument ||
		request.target.URLID != source.JobID || request.target.CanonicalURL != source.CanonicalURL {
		return DocumentTranscript{}, ErrDigestInputMismatch
	}
	return DocumentTranscript{
		sourceJobID: source.JobID, sourceURL: source.CanonicalURL, lease: request.lease,
		requests: []SuccessfulDocumentRequest{request}, initialized: true,
	}, nil
}

func (transcript DocumentTranscript) AppendRedirect(request SuccessfulDocumentRequest) (DocumentTranscript, error) {
	if !transcript.initialized || len(transcript.requests) == 0 || len(transcript.requests) >= int(MaxRequestStartsPerRun) {
		return DocumentTranscript{}, ErrInvalidOutput
	}
	if err := validateSuccessfulDocumentRequest(request); err != nil {
		return DocumentTranscript{}, err
	}
	previous := transcript.requests[len(transcript.requests)-1]
	if request.kind != RequestRedirect || request.lease != transcript.lease ||
		request.requestOrdinal != previous.requestOrdinal+1 ||
		request.jobRequestStarts != previous.jobRequestStarts+1 ||
		request.redisStartedAtMS < previous.redisStartedAtMS ||
		request.runRequestStarts < previous.runRequestStarts ||
		request.groupRequestStarts < previous.groupRequestStarts {
		return DocumentTranscript{}, ErrDigestInputMismatch
	}
	next := transcript
	next.requests = append([]SuccessfulDocumentRequest(nil), transcript.requests...)
	next.requests = append(next.requests, request)
	if _, err := transcriptAliases(next, 0); err != nil {
		return DocumentTranscript{}, err
	}
	return next, nil
}

func ParseFinalDocumentWitness(lease LeaseIdentity, raw any) (FinalDocumentWitness, error) {
	if err := validateLeaseIdentity(lease); err != nil {
		return FinalDocumentWitness{}, err
	}
	values, ok := responseArray(raw)
	if !ok || len(values) != 5 {
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
	return FinalDocumentWitness{
		lease: lease, redisStartedAtMS: RedisMilliseconds(startedAt), target: target,
		targetDigest: targetDigest, initialized: true,
	}, nil
}

func NewRenderPolicyAuthorization(runPinnedDigest, loadedPolicyDigest Digest, enabledRuleIDs []string) (RenderPolicyAuthorization, error) {
	if err := validateDigest(runPinnedDigest); err != nil {
		return RenderPolicyAuthorization{}, err
	}
	if err := validateDigest(loadedPolicyDigest); err != nil || loadedPolicyDigest != runPinnedDigest {
		return RenderPolicyAuthorization{}, ErrDigestInputMismatch
	}
	rules := make(map[string]struct{}, len(enabledRuleIDs))
	for _, rule := range enabledRuleIDs {
		if len(rule) == 0 || len(rule) > MaxRenderPolicyRuleIDBytes || !utf8.ValidString(rule) || containsControl(rule) {
			return RenderPolicyAuthorization{}, ErrInvalidOutput
		}
		if _, exists := rules[rule]; exists {
			return RenderPolicyAuthorization{}, ErrDuplicateSemanticKey
		}
		rules[rule] = struct{}{}
	}
	return RenderPolicyAuthorization{digest: runPinnedDigest, enabledRules: rules, initialized: true}, nil
}

func NewOutputContext(source SourceJob, transcript DocumentTranscript, witness FinalDocumentWitness, renderPolicy RenderPolicyAuthorization) (OutputContext, error) {
	if _, err := sourceJobRecord(source); err != nil {
		return OutputContext{}, err
	}
	if !transcript.initialized || !witness.initialized || !renderPolicy.initialized ||
		transcript.sourceJobID != source.JobID || transcript.sourceURL != source.CanonicalURL ||
		transcript.lease.JobID != source.JobID || witness.lease != transcript.lease || len(transcript.requests) == 0 {
		return OutputContext{}, ErrDigestInputMismatch
	}
	finalRequest := transcript.requests[len(transcript.requests)-1]
	if witness.redisStartedAtMS != finalRequest.redisStartedAtMS || witness.target != finalRequest.target ||
		witness.targetDigest == "" {
		return OutputContext{}, ErrDigestInputMismatch
	}
	aliases, err := transcriptAliases(transcript, source.Depth)
	if err != nil {
		return OutputContext{}, err
	}
	return OutputContext{
		jobID:        source.JobID,
		leaseFence:   transcript.lease.Fence,
		finalTarget:  finalRequest.target,
		lastCrawled:  formatRedisLastCrawled(finalRequest.redisStartedAtMS),
		aliases:      aliases,
		renderPolicy: renderPolicy,
		initialized:  true,
	}, nil
}

func validateSuccessfulDocumentRequest(request SuccessfulDocumentRequest) error {
	if !request.initialized || request.kind != RequestDocument && request.kind != RequestRedirect ||
		request.requestOrdinal == 0 || request.requestOrdinal > MaxExactInteger || request.redisStartedAtMS == 0 ||
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
	return nil
}

func transcriptAliases(transcript DocumentTranscript, depth uint64) ([]outputAlias, error) {
	aliasesByID := make(map[JobID]outputAlias, len(transcript.requests))
	for _, request := range transcript.requests {
		if existing, ok := aliasesByID[request.target.URLID]; ok && existing.CanonicalURL != request.target.CanonicalURL {
			return nil, ErrURLIdentityMismatch
		}
		aliasesByID[request.target.URLID] = outputAlias{URLID: request.target.URLID, CanonicalURL: request.target.CanonicalURL, Depth: depth}
	}
	if len(aliasesByID) == 0 || len(aliasesByID) > MaxAliasesPerJob {
		return nil, ErrInputLimitExceeded
	}
	aliases := make([]outputAlias, 0, len(aliasesByID))
	for _, alias := range aliasesByID {
		aliases = append(aliases, alias)
	}
	sort.Slice(aliases, func(left, right int) bool { return string(aliases[left].URLID) < string(aliases[right].URLID) })
	return aliases, nil
}

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
	if !context.initialized {
		return "", ErrInvalidOutput
	}
	pageRecord, err := outputPageRecord(context, output.Page)
	if err != nil {
		return "", err
	}
	outlinkRecords, err := outputOutlinkRecords(output.Page.NormalizedURL, output.Outlinks)
	if err != nil {
		return "", err
	}
	imageRecords, err := outputImageRecords(output.Images)
	if err != nil {
		return "", err
	}
	discoveryRecords, err := outputDiscoveryRecords(output.Discoveries)
	if err != nil {
		return "", err
	}
	aliasRecords, err := outputAliasRecords(context.aliases)
	if err != nil {
		return "", err
	}

	pageSection, err := EncodeSection("page", []Record{pageRecord})
	if err != nil {
		return "", err
	}
	outlinksSection, err := EncodeSection("outlinks", outlinkRecords)
	if err != nil {
		return "", err
	}
	imagesSection, err := EncodeSection("images", imageRecords)
	if err != nil {
		return "", err
	}
	discoveriesSection, err := EncodeSection("discoveries", discoveryRecords)
	if err != nil {
		return "", err
	}
	aliasesSection, err := EncodeSection("aliases", aliasRecords)
	if err != nil {
		return "", err
	}
	return digestEncoded(
		"mifolyo:crawl-output:v2",
		pageSection,
		outlinksSection,
		imagesSection,
		discoveriesSection,
		aliasesSection,
	), nil
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
	if !context.initialized {
		return nil, ErrInvalidOutput
	}
	if _, err := requireCanonicalURL(page.NormalizedURL); err != nil {
		return nil, err
	}
	if page.NormalizedURL != context.finalTarget.CanonicalURL {
		return nil, ErrDigestInputMismatch
	}
	if !utf8.Valid(page.HTML) || !utf8.Valid(page.OriginalHTML) || len(page.HTML) > MaxPageBlobBytes || len(page.OriginalHTML) > MaxPageBlobBytes || len(page.HTML)+len(page.OriginalHTML) > MaxCombinedHTMLBytes {
		return nil, ErrInvalidOutput
	}
	if err := validateContentType(page.ContentType); err != nil {
		return nil, err
	}
	if page.StatusCode < 100 || page.StatusCode > 399 {
		return nil, ErrInvalidOutput
	}
	if page.Rendered {
		if len(page.OriginalHTML) == 0 || len(page.RenderPolicyRule) == 0 || len(page.RenderPolicyRule) > MaxRenderPolicyRuleIDBytes || !utf8.ValidString(page.RenderPolicyRule) || containsControl(page.RenderPolicyRule) {
			return nil, ErrInvalidOutput
		}
		if !context.renderPolicy.initialized || page.RenderPolicyDigest != context.renderPolicy.digest {
			return nil, ErrDigestInputMismatch
		}
		if _, allowed := context.renderPolicy.enabledRules[page.RenderPolicyRule]; !allowed {
			return nil, ErrInvalidOutput
		}
	} else if len(page.OriginalHTML) != 0 || page.RenderPolicyRule != "" || page.RenderPolicyDigest != "" {
		return nil, ErrInvalidOutput
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
		return nil, ErrInputLimitExceeded
	}
	ordered := append([]string(nil), outlinks...)
	sort.Strings(ordered)
	records := make([]Record, 0, len(ordered))
	for index, targetURL := range ordered {
		if _, err := requireCanonicalURL(targetURL); err != nil {
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
		return nil, ErrInputLimitExceeded
	}
	ordered := append([]OutputImage(nil), images...)
	sort.Slice(ordered, func(left, right int) bool {
		return ordered[left].NormalizedSourceURL < ordered[right].NormalizedSourceURL
	})
	records := make([]Record, 0, len(ordered))
	for index, image := range ordered {
		if _, err := requireCanonicalURL(image.NormalizedSourceURL); err != nil {
			return nil, err
		}
		if !utf8.ValidString(image.Alt) || len(image.Alt) > MaxImageAltBytes {
			return nil, ErrInvalidOutput
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
		return nil, ErrInputLimitExceeded
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
		return nil, ErrInputLimitExceeded
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
	if value == "" || len(value) > 1024 || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return ErrInvalidOutput
	}
	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil || !strings.EqualFold(mediaType, "text/html") || len(parameters) > 1 {
		return ErrInvalidOutput
	}
	if len(parameters) == 1 {
		charset, ok := parameters["charset"]
		if !ok || !strings.EqualFold(charset, "utf-8") {
			return ErrInvalidOutput
		}
	}
	return nil
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}
