package crawljobsv2

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unicode/utf8"
)

type vectorTarget struct {
	URLID        string `json:"url_id"`
	CanonicalURL string `json:"canonical_url"`
}

type vectorDecision struct {
	RequestKind string `json:"request_kind"`
	Target      string `json:"target"`
	Depth       uint64 `json:"depth"`
	GroupID     string `json:"group_id"`
	RateScopeID string `json:"rate_scope_id"`
	Concurrency uint64 `json:"concurrency"`
	IntervalMS  uint64 `json:"interval_ms"`
}

type vectorSourceJob struct {
	Target      string `json:"target"`
	ScoreText   string `json:"score_text"`
	Depth       uint64 `json:"depth"`
	GroupID     string `json:"group_id"`
	RateScopeID string `json:"rate_scope_id"`
	Decision    string `json:"decision"`
}

type digestVectorCase struct {
	Name     string          `json:"name"`
	Kind     string          `json:"kind"`
	Input    json.RawMessage `json:"input"`
	Expected json.RawMessage `json:"expected"`
}

type digestVectorNegative struct {
	Name                   string          `json:"name"`
	Kind                   string          `json:"kind"`
	Input                  json.RawMessage `json:"input"`
	ExpectedRejectionClass string          `json:"expected_rejection_class"`
}

type digestVectorFixture struct {
	FixtureVersion   uint64 `json:"fixture_version"`
	SuiteName        string `json:"suite_name"`
	BaselineCaseName string `json:"baseline_case_name"`
	Identities       struct {
		RunID             string `json:"run_id"`
		JobID             string `json:"job_id"`
		OwnerID           string `json:"owner_id"`
		AlternateOwnerID  string `json:"alternate_owner_id"`
		LeaseToken        string `json:"lease_token"`
		Fence             uint64 `json:"fence"`
		RateScopeID       string `json:"rate_scope_id"`
		CrawlPolicyDigest string `json:"crawl_policy_sha256"`
	} `json:"identities"`
	Targets         map[string]vectorTarget   `json:"targets"`
	PolicyDecisions map[string]vectorDecision `json:"policy_decisions"`
	PolicyGroups    []struct {
		GroupID           string `json:"group_id"`
		RateScopeID       string `json:"rate_scope_id"`
		RequestStartLimit uint64 `json:"request_start_limit"`
		Concurrency       uint64 `json:"concurrency"`
		IntervalMS        uint64 `json:"interval_ms"`
	} `json:"policy_groups"`
	SourceJobs  []vectorSourceJob `json:"source_jobs"`
	Reservation struct {
		RequestOrdinal uint64 `json:"request_ordinal"`
		Target         string `json:"target"`
		Decision       string `json:"decision"`
	} `json:"reservation"`
	TryClaim struct {
		SourceJobIndex     int    `json:"source_job_index"`
		ExpectedPriorFence uint64 `json:"expected_prior_fence"`
		RequestOrdinal     uint64 `json:"request_ordinal"`
		Target             string `json:"target"`
		Decision           string `json:"decision"`
	} `json:"try_claim"`
	OutputContext struct {
		SourceJobIndex int `json:"source_job_index"`
		Requests       []struct {
			RequestKind string `json:"request_kind"`
			Target      string `json:"target"`
			StartedAtMS uint64 `json:"started_at_ms"`
		} `json:"requests"`
	} `json:"output_context"`
	Output struct {
		Page struct {
			NormalizedTarget   string `json:"normalized_target"`
			HTML               string `json:"html"`
			OriginalHTML       string `json:"original_html"`
			ContentType        string `json:"content_type"`
			StatusCode         uint16 `json:"status_code"`
			Rendered           bool   `json:"rendered"`
			RenderPolicyRule   string `json:"render_policy_rule"`
			RenderPolicyDigest string `json:"render_policy_sha256"`
		} `json:"page"`
		Outlinks []string `json:"outlinks"`
		Images   []struct {
			NormalizedSourceURL string `json:"normalized_source_url"`
			Alt                 string `json:"alt"`
		} `json:"images"`
		Discoveries []vectorSourceJob `json:"discoveries"`
	} `json:"output"`
	TransitionReasons map[string]string `json:"transition_reasons"`
	Chunk             struct {
		Kind    string `json:"kind"`
		Ordinal uint64 `json:"ordinal"`
		Records []struct {
			TargetURL string `json:"target_url"`
		} `json:"records"`
	} `json:"chunk"`
	Expected struct {
		Framing struct {
			U641Hex    string `json:"u64_1_hex"`
			FAHex      string `json:"f_A_hex"`
			RecordHex  string `json:"record_hex"`
			SectionHex string `json:"section_hex"`
		} `json:"framing"`
		ScopeIDs              map[string]string `json:"scope_ids"`
		URLIDs                map[string]string `json:"url_ids"`
		TargetDigests         map[string]string `json:"target_digests"`
		PolicyDecisionDigests map[string]string `json:"policy_decision_digests"`
		PolicyGroupMapDigest  string            `json:"policy_group_map_sha256"`
		TokenDigest           string            `json:"token_digest"`
		ReservationID         string            `json:"reservation_id"`
		SourceDigest          string            `json:"source_sha256"`
		OutputDigest          string            `json:"output_digest"`
		PublicationID         string            `json:"publication_id"`
		CommitID              string            `json:"commit_id"`
		ChunkDigest           string            `json:"chunk_digest"`
		TransitionPayloads    map[string]string `json:"transition_payload_digests"`
		TransitionIDs         map[string]string `json:"transition_ids"`
	} `json:"expected"`
	Cases           []digestVectorCase     `json:"cases"`
	NegativeVectors []digestVectorNegative `json:"negative_vectors"`
}

func loadDigestVectorFixture(t *testing.T) digestVectorFixture {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate fixture test source")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../../../../../contracts/crawl-jobs-v2/digest-vectors.json"))
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read shared vectors: %v", err)
	}
	if !json.Valid(contents) {
		t.Fatal("shared vector file is not valid JSON")
	}
	if !utf8.Valid(contents) {
		t.Fatal("shared vector file is not valid UTF-8")
	}
	var fixture digestVectorFixture
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatalf("decode shared vectors: %v", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		t.Fatalf("decode shared vectors: %v", err)
	}
	if fixture.FixtureVersion != 2 {
		t.Fatalf("fixture version = %d, want 2", fixture.FixtureVersion)
	}
	if fixture.SuiteName != "crawl-jobs-v2-digest-conformance" {
		t.Fatalf("fixture suite = %q", fixture.SuiteName)
	}
	if fixture.BaselineCaseName == "" {
		t.Fatal("fixture baseline case name is empty")
	}
	if len(fixture.Cases) == 0 || len(fixture.NegativeVectors) == 0 {
		t.Fatalf("fixture case arrays must be non-empty: positive=%d negative=%d", len(fixture.Cases), len(fixture.NegativeVectors))
	}
	seen := map[string]struct{}{fixture.BaselineCaseName: {}}
	for _, vector := range fixture.Cases {
		if vector.Kind == "" || !rawJSONObject(vector.Input) || !rawJSONObject(vector.Expected) {
			t.Fatalf("positive fixture case %q has an invalid kind/input/expected shape", vector.Name)
		}
	}
	for _, vector := range fixture.NegativeVectors {
		if vector.Kind == "" || !rawJSONObject(vector.Input) || !validFixtureRejectionClass(vector.ExpectedRejectionClass) {
			t.Fatalf("negative fixture case %q has an invalid kind/input/rejection shape", vector.Name)
		}
	}
	for _, vector := range appendCaseNames(fixture.Cases, fixture.NegativeVectors) {
		if vector == "" {
			t.Fatal("fixture contains an empty case name")
		}
		if _, duplicate := seen[vector]; duplicate {
			t.Fatalf("fixture contains duplicate case name %q", vector)
		}
		seen[vector] = struct{}{}
	}
	return fixture
}

func rawJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) >= 2 && trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}'
}

func validFixtureRejectionClass(value string) bool {
	if value == "" || value[0] < 'A' || value[0] > 'Z' {
		return false
	}
	for index := 1; index < len(value); index++ {
		if value[index] != '_' && (value[index] < 'A' || value[index] > 'Z') && (value[index] < '0' || value[index] > '9') {
			return false
		}
	}
	return true
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return errors.New("unexpected trailing JSON value")
	}
	return err
}

func appendCaseNames(cases []digestVectorCase, negatives []digestVectorNegative) []string {
	names := make([]string, 0, len(cases)+len(negatives))
	for _, vector := range cases {
		names = append(names, vector.Name)
	}
	for _, vector := range negatives {
		names = append(names, vector.Name)
	}
	return names
}

func vectorLease(t *testing.T, fixture digestVectorFixture) LeaseIdentity {
	t.Helper()
	runID, err := ParseRunID(fixture.Identities.RunID)
	if err != nil {
		t.Fatal(err)
	}
	jobID, err := ParseJobID(fixture.Identities.JobID)
	if err != nil {
		t.Fatal(err)
	}
	ownerID, err := ParseOwnerID(fixture.Identities.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	token, err := ParseLeaseToken(fixture.Identities.LeaseToken)
	if err != nil {
		t.Fatal(err)
	}
	fence, err := NewFence(fixture.Identities.Fence)
	if err != nil {
		t.Fatal(err)
	}
	return LeaseIdentity{RunID: runID, JobID: jobID, OwnerID: ownerID, Fence: fence, Token: token}
}

func vectorTargetValue(t *testing.T, fixture digestVectorFixture, name string) RequestTarget {
	t.Helper()
	vector, ok := fixture.Targets[name]
	if !ok {
		t.Fatalf("unknown fixture target %q", name)
	}
	jobID, err := ParseJobID(vector.URLID)
	if err != nil {
		t.Fatal(err)
	}
	return RequestTarget{URLID: jobID, CanonicalURL: vector.CanonicalURL}
}

func vectorDecisionValue(t *testing.T, fixture digestVectorFixture, name string) PolicyDecision {
	t.Helper()
	vector, ok := fixture.PolicyDecisions[name]
	if !ok {
		t.Fatalf("unknown fixture decision %q", name)
	}
	kind, err := ParseRequestKind(vector.RequestKind)
	if err != nil {
		t.Fatal(err)
	}
	groupID, err := ParseGroupID(vector.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	rateScopeID, err := ParseRateScopeID(vector.RateScopeID)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := NewPolicyDecision(PolicyDecisionInput{
		RequestKind: kind, Target: vectorTargetValue(t, fixture, vector.Target), Depth: vector.Depth,
		GroupID: groupID, RateScopeID: rateScopeID,
		GroupConcurrency: vector.Concurrency, GroupIntervalMS: vector.IntervalMS,
		OriginConcurrency: vector.Concurrency, OriginIntervalMS: vector.IntervalMS,
	})
	if err != nil {
		t.Fatal(err)
	}
	return decision
}

func vectorSourceJobValue(t *testing.T, fixture digestVectorFixture, index int) SourceJob {
	t.Helper()
	if index < 0 || index >= len(fixture.SourceJobs) {
		t.Fatalf("source index %d out of bounds", index)
	}
	vector := fixture.SourceJobs[index]
	target := vectorTargetValue(t, fixture, vector.Target)
	score, err := ParseScoreText(vector.ScoreText)
	if err != nil {
		t.Fatal(err)
	}
	groupID, err := ParseGroupID(vector.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	rateScopeID, err := ParseRateScopeID(vector.RateScopeID)
	if err != nil {
		t.Fatal(err)
	}
	return SourceJob{
		JobID: target.URLID, CanonicalURL: target.CanonicalURL, ScoreText: score, Depth: vector.Depth,
		GroupID: groupID, RateScopeID: rateScopeID, Decision: vectorDecisionValue(t, fixture, vector.Decision),
	}
}

func vectorReservationValue(t *testing.T, fixture digestVectorFixture, claim bool) ReservationIntent {
	t.Helper()
	vector := fixture.Reservation
	if claim {
		vector.RequestOrdinal = fixture.TryClaim.RequestOrdinal
		vector.Target = fixture.TryClaim.Target
		vector.Decision = fixture.TryClaim.Decision
	}
	digest, err := ParseDigest(fixture.Identities.CrawlPolicyDigest)
	if err != nil {
		t.Fatal(err)
	}
	return ReservationIntent{
		Lease: vectorLease(t, fixture), RequestOrdinal: vector.RequestOrdinal,
		Target: vectorTargetValue(t, fixture, vector.Target), CrawlPolicyDigest: digest,
		Decision: vectorDecisionValue(t, fixture, vector.Decision),
	}
}

func vectorOutputContextValue(t *testing.T, fixture digestVectorFixture) OutputContext {
	return vectorOutputContextWith(t, fixture, len(fixture.OutputContext.Requests), nil)
}

func vectorOutputContextWith(t *testing.T, fixture digestVectorFixture, requestCount int, enabledRenderRules []string) OutputContext {
	t.Helper()
	if requestCount < 1 || requestCount > len(fixture.OutputContext.Requests) {
		t.Fatalf("output-context request count %d out of bounds", requestCount)
	}
	lease := vectorLease(t, fixture)
	source := vectorSourceJobValue(t, fixture, fixture.OutputContext.SourceJobIndex)
	crawlPolicyDigest, err := ParseDigest(fixture.Identities.CrawlPolicyDigest)
	if err != nil {
		t.Fatal(err)
	}
	var transcript DocumentTranscript
	for index, vector := range fixture.OutputContext.Requests[:requestCount] {
		kind, err := ParseRequestKind(vector.RequestKind)
		if err != nil {
			t.Fatal(err)
		}
		target := vectorTargetValue(t, fixture, vector.Target)
		decision, err := NewPolicyDecision(PolicyDecisionInput{
			RequestKind: kind, Target: target, Depth: source.Depth,
			GroupID: source.GroupID, RateScopeID: source.RateScopeID,
			GroupConcurrency: source.Decision.GroupConcurrency, GroupIntervalMS: source.Decision.GroupIntervalMS,
			OriginConcurrency: source.Decision.OriginConcurrency, OriginIntervalMS: source.Decision.OriginIntervalMS,
		})
		if err != nil {
			t.Fatal(err)
		}
		intent := ReservationIntent{
			Lease: lease, RequestOrdinal: uint64(index + 1), Target: target,
			CrawlPolicyDigest: crawlPolicyDigest, Decision: decision,
		}
		reservationID, err := DeriveReservationID(intent)
		if err != nil {
			t.Fatal(err)
		}
		starts := uint64(index + 1)
		response, err := ParseStartRequestResponse(intent, []string{
			string(StatusStarted), canonicalDecimal(vector.StartedAtMS), string(reservationID),
			canonicalDecimal(vector.StartedAtMS), "1", canonicalDecimal(starts),
			canonicalDecimal(starts), canonicalDecimal(starts), "1",
		})
		if err != nil {
			t.Fatal(err)
		}
		permit, err := response.IOPermit()
		if err != nil {
			t.Fatal(err)
		}
		evidence, err := NewSuccessfulDocumentRequest(permit)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			transcript, err = NewDocumentTranscript(source, evidence)
		} else {
			transcript, err = transcript.AppendRedirect(evidence)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	last := fixture.OutputContext.Requests[requestCount-1]
	finalTarget := vectorTargetValue(t, fixture, last.Target)
	finalDigest, err := DeriveTargetDigest(finalTarget)
	if err != nil {
		t.Fatal(err)
	}
	witness, err := ParseFinalDocumentWitness(lease, []string{
		canonicalDecimal(last.StartedAtMS), canonicalDecimal(uint64(lease.Fence)),
		string(finalTarget.URLID), finalTarget.CanonicalURL, string(finalDigest),
	})
	if err != nil {
		t.Fatal(err)
	}
	renderPolicy, err := NewRenderPolicyAuthorization(crawlPolicyDigest, crawlPolicyDigest, enabledRenderRules)
	if err != nil {
		t.Fatal(err)
	}
	context, err := NewOutputContext(source, transcript, witness, renderPolicy)
	if err != nil {
		t.Fatal(err)
	}
	return context
}

func vectorOutputValue(t *testing.T, fixture digestVectorFixture) CrawlOutput {
	t.Helper()
	pageTarget := vectorTargetValue(t, fixture, fixture.Output.Page.NormalizedTarget)
	page := OutputPage{
		NormalizedURL: pageTarget.CanonicalURL,
		HTML:          []byte(fixture.Output.Page.HTML), OriginalHTML: []byte(fixture.Output.Page.OriginalHTML),
		ContentType: fixture.Output.Page.ContentType, StatusCode: fixture.Output.Page.StatusCode,
		Rendered: fixture.Output.Page.Rendered, RenderPolicyRule: fixture.Output.Page.RenderPolicyRule,
	}
	if fixture.Output.Page.RenderPolicyDigest != "" {
		digest, err := ParseDigest(fixture.Output.Page.RenderPolicyDigest)
		if err != nil {
			t.Fatal(err)
		}
		page.RenderPolicyDigest = digest
	}
	images := make([]OutputImage, 0, len(fixture.Output.Images))
	for _, vector := range fixture.Output.Images {
		images = append(images, OutputImage{NormalizedSourceURL: vector.NormalizedSourceURL, Alt: vector.Alt})
	}
	discoveries := make([]OutputDiscovery, 0, len(fixture.Output.Discoveries))
	for _, vector := range fixture.Output.Discoveries {
		target := vectorTargetValue(t, fixture, vector.Target)
		score, err := ParseScoreText(vector.ScoreText)
		if err != nil {
			t.Fatal(err)
		}
		groupID, err := ParseGroupID(vector.GroupID)
		if err != nil {
			t.Fatal(err)
		}
		rateScopeID, err := ParseRateScopeID(vector.RateScopeID)
		if err != nil {
			t.Fatal(err)
		}
		discoveries = append(discoveries, OutputDiscovery{
			JobID: target.URLID, CanonicalURL: target.CanonicalURL, Depth: vector.Depth,
			ScoreText: score, GroupID: groupID, RateScopeID: rateScopeID,
			Decision: vectorDecisionValue(t, fixture, vector.Decision),
		})
	}
	return CrawlOutput{
		Page: page, Outlinks: append([]string(nil), fixture.Output.Outlinks...),
		Images: images, Discoveries: discoveries,
	}
}
