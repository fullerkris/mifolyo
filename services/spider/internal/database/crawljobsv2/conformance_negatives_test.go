package crawljobsv2

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

func TestSharedFixtureV2NegativeCases(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	harness := newFixtureConformanceHarness(t, fixture)
	consumed := make(map[string]struct{}, len(fixture.NegativeVectors))
	for _, vector := range fixture.NegativeVectors {
		vector := vector
		t.Run(vector.Name, func(t *testing.T) {
			actual := harness.runNegativeCase(t, vector)
			if actual == "" {
				t.Fatalf("negative fixture was accepted; expected %s", vector.ExpectedRejectionClass)
			}
			if actual != vector.ExpectedRejectionClass {
				t.Fatalf("rejection class = %s, want %s", actual, vector.ExpectedRejectionClass)
			}
		})
		consumed[vector.Name] = struct{}{}
	}
	if len(consumed) != len(fixture.NegativeVectors) {
		t.Fatalf("consumed %d of %d negative fixture cases", len(consumed), len(fixture.NegativeVectors))
	}
}

func (h *fixtureConformanceHarness) runNegativeCase(t *testing.T, vector digestVectorNegative) string {
	t.Helper()
	switch vector.Kind {
	case "transition_reason":
		input := decodeVectorPart[struct {
			Operation string `json:"operation"`
			Value     string `json:"value"`
		}](t, vector.Input, vector.Name+".input")
		operation, err := ParseOperationName(input.Operation)
		if err != nil {
			t.Fatalf("fixture transition operation: %v", err)
		}
		err = ValidateTransitionReason(operation, Reason(input.Value))
		if errors.Is(err, ErrInvalidTransitionReason) {
			return "INVALID_TRANSITION_REASON"
		}
		if err != nil {
			t.Fatalf("unexpected transition-reason error: %v", err)
		}
		return ""
	case "source_policy_request_kind":
		input := decodeVectorPart[struct {
			Value string `json:"value"`
		}](t, vector.Input, vector.Name+".input")
		job := vectorSourceJobValue(t, h.fixture, 0)
		job.Decision.RequestKind = RequestKind(input.Value)
		_, err := DeriveSourceDigest([]SourceJob{job})
		requireFixtureAPIRejection(t, err)
		if errors.Is(err, ErrDigestInputMismatch) {
			return "POLICY_BINDING_MISMATCH"
		}
		t.Fatalf("unexpected source policy error: %v", err)
	case "discovery_depth":
		input := decodeVectorPart[struct {
			Value uint64 `json:"value"`
		}](t, vector.Input, vector.Name+".input")
		output := vectorOutputValue(t, h.fixture)
		output.Discoveries[0].Depth = input.Value
		_, err := DeriveOutputDigest(vectorOutputContextValue(t, h.fixture), output)
		requireFixtureAPIRejection(t, err)
		if !errors.Is(err, ErrDigestInputMismatch) {
			t.Fatalf("unexpected discovery binding error: %v", err)
		}
		_, chunkErr := NewDiscoveriesStageChunk(mustFixtureDigest(t, h.fixture.Expected.CommitID), 0, output.Discoveries)
		requireFixtureAPIRejection(t, chunkErr)
		return "POLICY_BINDING_MISMATCH"
	case "output_normalized_target":
		input := decodeVectorPart[struct {
			Value string `json:"value"`
		}](t, vector.Input, vector.Name+".input")
		output := vectorOutputValue(t, h.fixture)
		output.Page.NormalizedURL = vectorTargetValue(t, h.fixture, input.Value).CanonicalURL
		_, err := DeriveOutputDigest(vectorOutputContextValue(t, h.fixture), output)
		requireFixtureAPIRejection(t, err)
		if errors.Is(err, ErrDigestInputMismatch) {
			return "OUTPUT_CONTEXT_MISMATCH"
		}
		t.Fatalf("unexpected output-context error: %v", err)
	case "score_text":
		input := decodeVectorPart[struct {
			Value string `json:"value"`
		}](t, vector.Input, vector.Name+".input")
		return fixtureScoreRejection(t, input.Value)
	case "lease_token":
		input := decodeVectorPart[struct {
			Value string `json:"value"`
		}](t, vector.Input, vector.Name+".input")
		if _, err := ParseLeaseToken(input.Value); errors.Is(err, ErrInvalidLeaseToken) {
			return "INVALID_LEASE_TOKEN"
		} else if err != nil {
			t.Fatalf("unexpected lease-token error: %v", err)
		}
		return ""
	case "redis_score":
		input := decodeVectorPart[struct {
			ScoreText  string `json:"score_text"`
			RedisValue string `json:"redis_value"`
		}](t, vector.Input, vector.Name+".input")
		return fixtureRedisScoreRejection(t, input.ScoreText, input.RedisValue)
	case "canonical_url":
		input := decodeVectorPart[struct {
			Value string `json:"value"`
		}](t, vector.Input, vector.Name+".input")
		return fixtureCanonicalURLRejection(t, input.Value)
	case "reservation_target":
		input := decodeVectorPart[struct {
			Value string `json:"value"`
		}](t, vector.Input, vector.Name+".input")
		intent := vectorReservationValue(t, h.fixture, false)
		intent.Target = vectorTargetValue(t, h.fixture, input.Value)
		_, err := DeriveReservationID(intent)
		requireFixtureAPIRejection(t, err)
		if errors.Is(err, ErrDigestInputMismatch) {
			return "RESERVATION_TARGET_MISMATCH"
		}
		t.Fatalf("unexpected reservation target error: %v", err)
	case "output_mutation":
		input := decodeVectorPart[struct {
			Mutation string `json:"mutation"`
		}](t, vector.Input, vector.Name+".input")
		return h.fixtureOutputMutationRejection(t, input.Mutation)
	case "source_shape":
		input := decodeVectorPart[struct {
			Count int `json:"count"`
		}](t, vector.Input, vector.Name+".input")
		if input.Count < 0 {
			t.Fatal("negative source count")
		}
		_, err := DeriveSourceDigest(make([]SourceJob, input.Count))
		if input.Count > MaxJobsPerRun {
			requireFixtureAPIRejection(t, err)
			return "SOURCE_COUNT_LIMIT"
		}
		if err != nil {
			t.Fatalf("in-range source shape rejected: %v", err)
		}
		return ""
	case "section_shape":
		input := decodeVectorPart[struct {
			Section string `json:"section"`
			Count   int    `json:"count"`
		}](t, vector.Input, vector.Name+".input")
		return fixtureSectionShapeRejection(t, input.Section, input.Count)
	case "stage_chunk_mutation":
		input := decodeVectorPart[struct {
			Mutation string `json:"mutation"`
		}](t, vector.Input, vector.Name+".input")
		return h.fixtureStageMutationRejection(t, input.Mutation)
	case "fixture_type":
		input := decodeVectorPart[struct {
			Field string          `json:"field"`
			Value json.RawMessage `json:"value"`
		}](t, vector.Input, vector.Name+".input")
		return fixtureTypeRejection(t, input.Field, input.Value)
	case "json_text":
		input := decodeVectorPart[struct {
			Text string `json:"text"`
		}](t, vector.Input, vector.Name+".input")
		return strictJSONRejection([]byte(input.Text))
	case "u64":
		input := decodeVectorPart[struct {
			Decimal string `json:"decimal"`
		}](t, vector.Input, vector.Name+".input")
		return fixtureU64Rejection(input.Decimal)
	case "output_utf8":
		input := decodeVectorPart[struct {
			Field    string `json:"field"`
			BytesHex string `json:"bytes_hex"`
		}](t, vector.Input, vector.Name+".input")
		return h.fixtureOutputUTF8Rejection(t, input.Field, input.BytesHex)
	case "transition_replay":
		input := decodeVectorPart[struct {
			Mutation string `json:"mutation"`
		}](t, vector.Input, vector.Name+".input")
		return h.fixtureTransitionReplayRejection(t, input.Mutation)
	default:
		t.Fatalf("unsupported negative fixture kind %q", vector.Kind)
	}
	return ""
}

func requireFixtureAPIRejection(t testing.TB, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("Go package API accepted a negative fixture")
	}
}

func fixtureScoreRejection(t testing.TB, value string) string {
	t.Helper()
	_, err := ParseScoreText(value)
	if err == nil {
		return ""
	}
	if !errors.Is(err, ErrInvalidScoreText) {
		t.Fatalf("unexpected score error: %v", err)
	}
	if !scoreTextPattern.MatchString(value) {
		return "INVALID_SCORE_SYNTAX"
	}
	parsed, parseErr := strconv.ParseFloat(value, 64)
	if parseErr != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return "SCORE_NOT_FINITE"
	}
	if parsed < -1000 || parsed > 10000 {
		return "SCORE_OUT_OF_RANGE"
	}
	t.Fatalf("canonical score %q was rejected without a conformance class", value)
	return ""
}

var fixtureRedisFloatPattern = regexp.MustCompile(`(?i)^[+-]?(?:(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?|inf(?:inity)?|nan)$`)

func fixtureRedisScoreRejection(t testing.TB, scoreText, redisValue string) string {
	t.Helper()
	score, err := ParseScoreText(scoreText)
	if err != nil {
		t.Fatalf("negative Redis-score vector has invalid canonical score: %v", err)
	}
	apiErr := ValidateRedisScore(score, redisValue)
	if apiErr == nil {
		return ""
	}
	if !errors.Is(apiErr, ErrInvalidRedisScore) {
		t.Fatalf("unexpected Redis score API error: %v", apiErr)
	}
	if !fixtureRedisFloatPattern.MatchString(redisValue) {
		return "INVALID_REDIS_SCORE"
	}
	parsed, ok := parseFixtureRedisFloat(redisValue)
	if !ok {
		return "INVALID_REDIS_SCORE"
	}
	if math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return "REDIS_SCORE_NOT_FINITE"
	}
	canonical, err := score.Float64()
	if err != nil {
		t.Fatal(err)
	}
	if math.Float64bits(parsed) != math.Float64bits(canonical) {
		return "SCORE_BINARY64_MISMATCH"
	}
	t.Fatalf("equivalent finite Redis score was rejected: %v", apiErr)
	return ""
}

func parseFixtureRedisFloat(value string) (float64, bool) {
	lower := strings.ToLower(value)
	unsigned := strings.TrimPrefix(strings.TrimPrefix(lower, "+"), "-")
	sign := 1.0
	if strings.HasPrefix(lower, "-") {
		sign = -1
	}
	switch unsigned {
	case "nan":
		return math.NaN(), true
	case "inf", "infinity":
		return math.Inf(int(sign)), true
	default:
		parsed, err := strconv.ParseFloat(value, 64)
		return parsed, err == nil
	}
}

func fixtureCanonicalURLRejection(t testing.TB, value string) string {
	t.Helper()
	if len(value) > MaxCanonicalURLBytes {
		if _, err := utils.CanonicalizeURLV1(value); !errors.Is(err, utils.ErrURLTooLong) {
			t.Fatalf("over-limit URL package error = %v", err)
		}
		return "URL_TOO_LONG"
	}
	if !utf8.ValidString(value) {
		return "INVALID_UTF8"
	}
	if strings.Contains(value, "#") {
		identity, err := utils.CanonicalizeURLV1(value)
		if err == nil && identity.CanonicalURL == value {
			t.Fatal("fragment-bearing URL remained canonical")
		}
		return "URL_FRAGMENT_FORBIDDEN"
	}
	parsed, parseErr := url.Parse(value)
	if parseErr == nil && parsed.User != nil {
		if _, err := utils.CanonicalizeURLV1(value); !errors.Is(err, utils.ErrUserinfoForbidden) {
			t.Fatalf("userinfo URL package error = %v", err)
		}
		return "URL_USERINFO_FORBIDDEN"
	}
	if parseErr == nil && net.ParseIP(parsed.Hostname()) != nil {
		identity, err := utils.CanonicalizeURLV1(value)
		if err != nil {
			t.Fatalf("canonicalize IP literal for admission decision: %v", err)
		}
		if err := utils.RequireStaticCrawlEligibility(identity); err == nil || utils.CrawlAdmissionErrorCode(err) != utils.CrawlRejectionIPLiteral {
			t.Fatalf("IP literal admission error = %v", err)
		}
		return "URL_IP_LITERAL_FORBIDDEN"
	}
	if parseErr == nil {
		port := parsed.Port()
		if parsed.Scheme == "https" && port == "443" || parsed.Scheme == "http" && port == "80" {
			identity, err := utils.CanonicalizeURLV1(value)
			if err != nil || identity.CanonicalURL == value {
				t.Fatalf("default-port normalization failed: canonical=%q err=%v", identity.CanonicalURL, err)
			}
			return "URL_DEFAULT_PORT_FORBIDDEN"
		}
	}
	identity, err := utils.CanonicalizeURLV1(value)
	if err != nil || identity.CanonicalURL != value {
		return "CANONICAL_URL_NOT_CANONICAL"
	}
	return ""
}

func (h *fixtureConformanceHarness) fixtureOutputMutationRejection(t *testing.T, mutation string) string {
	t.Helper()
	context := vectorOutputContextValue(t, h.fixture)
	output := cloneFixtureOutput(vectorOutputValue(t, h.fixture))
	class := ""
	switch mutation {
	case "outlinks_one_over":
		output.Outlinks = make([]string, MaxOutlinksPerJob+1)
		for index := range output.Outlinks {
			output.Outlinks[index] = fmtURL("overflow.example.com", index)
		}
	case "discoveries_one_over":
		output.Discoveries = make([]OutputDiscovery, MaxDiscoveriesPerJob+1)
		for index := range output.Discoveries {
			output.Discoveries[index] = vectorOutputValue(t, h.fixture).Discoveries[0]
		}
	case "images_one_over":
		output.Images = make([]OutputImage, MaxImagesPerPage+1)
		for index := range output.Images {
			output.Images[index] = OutputImage{NormalizedSourceURL: fmtURL("images.example.com", index)}
		}
	case "duplicate_outlink":
		output.Outlinks = append(output.Outlinks, output.Outlinks[0])
	case "duplicate_image":
		output.Images = append(output.Images, output.Images[0])
	case "duplicate_discovery":
		output.Discoveries = append(output.Discoveries, output.Discoveries[0])
	case "html_one_over":
		output.Page.HTML = bytes.Repeat([]byte{'H'}, MaxPageBlobBytes+1)
	case "original_html_one_over":
		output.Page.OriginalHTML = bytes.Repeat([]byte{'O'}, MaxPageBlobBytes+1)
	case "alt_one_over":
		output.Images[0].Alt = strings.Repeat("a", MaxImageAltBytes+1)
	case "content_type_empty":
		output.Page.ContentType = ""
	case "content_type_one_over":
		output.Page.ContentType = "text/html;" + strings.Repeat(" ", 1025)
	case "content_type_untrimmed":
		output.Page.ContentType = " text/html"
	case "content_type_non_html":
		output.Page.ContentType = "application/xhtml+xml"
	case "content_type_extra_parameter":
		output.Page.ContentType = "text/html;charset=utf-8;level=1"
	case "content_type_wrong_charset":
		output.Page.ContentType = "text/html;charset=iso-8859-1"
	case "render_false_with_original":
		output.Page.OriginalHTML = []byte("source")
	case "render_false_with_rule":
		output.Page.RenderPolicyRule = "render-main"
	case "render_false_with_digest":
		output.Page.RenderPolicyDigest = mustFixtureDigest(t, h.fixture.Identities.CrawlPolicyDigest)
	case "render_true_without_original", "render_true_without_rule", "render_true_rule_control", "render_true_bad_digest":
		context = vectorOutputContextWith(t, h.fixture, len(h.fixture.OutputContext.Requests), []string{"render-main"})
		output.Page.Rendered = true
		output.Page.OriginalHTML = []byte("source")
		output.Page.RenderPolicyRule = "render-main"
		output.Page.RenderPolicyDigest = mustFixtureDigest(t, h.fixture.Identities.CrawlPolicyDigest)
		switch mutation {
		case "render_true_without_original":
			output.Page.OriginalHTML = []byte{}
		case "render_true_without_rule":
			output.Page.RenderPolicyRule = ""
		case "render_true_rule_control":
			output.Page.RenderPolicyRule = "render\nmain"
		case "render_true_bad_digest":
			output.Page.RenderPolicyDigest = Digest(strings.Repeat("z", 64))
		}
	case "status_below":
		output.Page.StatusCode = 99
	case "status_above":
		output.Page.StatusCode = 400
	case "url_one_over":
		output.Outlinks = []string{fixtureSizedURL("too-long.example.com", "url", 0, MaxCanonicalURLBytes+1)}
	case "group_id_one_over":
		group := GroupID(strings.Repeat("g", MaxPolicyGroupIDBytes+1))
		output.Discoveries[0].GroupID = group
		output.Discoveries[0].Decision.GroupID = group
	case "target_id_mismatch":
		job := vectorSourceJobValue(t, h.fixture, 0)
		job.JobID = JobID(strings.Repeat("f", 64))
		_, err := DeriveSourceDigest([]SourceJob{job})
		requireFixtureAPIRejection(t, err)
		if errors.Is(err, ErrURLIdentityMismatch) {
			return "URL_IDENTITY_MISMATCH"
		}
		t.Fatalf("unexpected target witness error: %v", err)
	default:
		t.Fatalf("unsupported output mutation %q", mutation)
	}
	class = classifyFixtureOutput(output, context)
	_, err := DeriveOutputDigest(context, output)
	if class != "" {
		requireFixtureAPIRejection(t, err)
	} else if err != nil {
		t.Fatalf("output classifier accepted value rejected by package API: %v", err)
	}
	return class
}

func classifyFixtureOutput(output CrawlOutput, context OutputContext) string {
	if len(output.Outlinks) > MaxOutlinksPerJob || len(output.Discoveries) > MaxDiscoveriesPerJob || len(output.Images) > MaxImagesPerPage {
		return "OUTPUT_COUNT_LIMIT"
	}
	if !utf8.Valid(output.Page.HTML) || !utf8.Valid(output.Page.OriginalHTML) {
		return "INVALID_UTF8"
	}
	if len(output.Page.HTML) > MaxPageBlobBytes || len(output.Page.OriginalHTML) > MaxPageBlobBytes {
		return "PAGE_BLOB_LIMIT"
	}
	if len(output.Page.HTML)+len(output.Page.OriginalHTML) > MaxCombinedHTMLBytes {
		return "COMBINED_HTML_LIMIT"
	}
	if len(output.Page.ContentType) > 1024 {
		return "CONTENT_TYPE_LIMIT"
	}
	if validateContentType(output.Page.ContentType) != nil {
		return "INVALID_CONTENT_TYPE"
	}
	if output.Page.StatusCode < 100 || output.Page.StatusCode > 399 {
		return "STATUS_CODE_RANGE"
	}
	if output.Page.Rendered {
		if output.Page.RenderPolicyRule == "" || containsControl(output.Page.RenderPolicyRule) {
			return "INVALID_RENDER_RELATION"
		}
		if len(output.Page.RenderPolicyRule) > MaxRenderPolicyRuleIDBytes {
			return "RENDER_RULE_LIMIT"
		}
		if _, err := ParseDigest(string(output.Page.RenderPolicyDigest)); err != nil {
			return "INVALID_RENDER_POLICY_DIGEST"
		}
		if len(output.Page.OriginalHTML) == 0 {
			return "INVALID_RENDER_RELATION"
		}
	} else if len(output.Page.OriginalHTML) != 0 || output.Page.RenderPolicyRule != "" || output.Page.RenderPolicyDigest != "" {
		return "INVALID_RENDER_RELATION"
	}
	if !context.initialized || output.Page.NormalizedURL != context.finalTarget.CanonicalURL {
		return "OUTPUT_CONTEXT_MISMATCH"
	}
	orderedOutlinks := append([]string(nil), output.Outlinks...)
	sort.Strings(orderedOutlinks)
	for index, value := range orderedOutlinks {
		if !utf8.ValidString(value) {
			return "INVALID_UTF8"
		}
		if len(value) > MaxCanonicalURLBytes {
			return "URL_TOO_LONG"
		}
		if index > 0 && value == orderedOutlinks[index-1] {
			return "DUPLICATE_OUTLINK"
		}
	}
	orderedImages := append([]OutputImage(nil), output.Images...)
	sort.Slice(orderedImages, func(left, right int) bool {
		return orderedImages[left].NormalizedSourceURL < orderedImages[right].NormalizedSourceURL
	})
	for index, image := range orderedImages {
		if !utf8.ValidString(image.Alt) || !utf8.ValidString(image.NormalizedSourceURL) {
			return "INVALID_UTF8"
		}
		if len(image.Alt) > MaxImageAltBytes {
			return "IMAGE_ALT_LIMIT"
		}
		if index > 0 && image.NormalizedSourceURL == orderedImages[index-1].NormalizedSourceURL {
			return "DUPLICATE_IMAGE"
		}
	}
	orderedDiscoveries := append([]OutputDiscovery(nil), output.Discoveries...)
	sort.Slice(orderedDiscoveries, func(left, right int) bool { return orderedDiscoveries[left].JobID < orderedDiscoveries[right].JobID })
	for index, discovery := range orderedDiscoveries {
		if len(discovery.GroupID) > MaxPolicyGroupIDBytes {
			return "GROUP_ID_LIMIT"
		}
		if index > 0 && discovery.JobID == orderedDiscoveries[index-1].JobID {
			return "DUPLICATE_DISCOVERY"
		}
	}
	return ""
}

func fmtURL(host string, index int) string {
	return "https://" + host + "/" + fmt.Sprintf("%03d", index)
}

func fixtureSectionShapeRejection(t testing.TB, section string, count int) string {
	t.Helper()
	switch section {
	case "page":
		if count != 1 {
			return "PAGE_SECTION_SHAPE"
		}
	case "outlinks":
		if count < 0 || count > MaxOutlinksPerJob {
			return "OUTPUT_COUNT_LIMIT"
		}
	case "images":
		if count < 0 || count > MaxImagesPerPage {
			return "OUTPUT_COUNT_LIMIT"
		}
	case "discoveries":
		if count < 0 || count > MaxDiscoveriesPerJob {
			return "OUTPUT_COUNT_LIMIT"
		}
	case "aliases":
		if count < 1 || count > MaxAliasesPerJob {
			return "ALIAS_COUNT_LIMIT"
		}
	default:
		t.Fatalf("unsupported output section %q", section)
	}
	return ""
}

func (h *fixtureConformanceHarness) fixtureStageMutationRejection(t *testing.T, mutation string) string {
	t.Helper()
	profile := h.outputProfile(t, "baseline")
	commitID := mustFixtureDigest(t, profile.result.CommitID)
	switch mutation {
	case "unknown_kind":
		_, err := newValidatedStageChunk(commitID, ChunkKind("generic"), 0, nil)
		requireFixtureAPIRejection(t, err)
		if !errors.Is(err, ErrUnknownChunkKind) {
			t.Fatalf("unknown chunk error = %v", err)
		}
		return "UNKNOWN_CHUNK_KIND"
	case "outlinks_65_records":
		values := make([]string, MaxNonBlobStageBatchRecords+1)
		for index := range values {
			values[index] = fmtURL("chunks.example.com", index)
		}
		_, err := NewOutlinksStageChunk(commitID, 0, profile.context, values)
		requireFixtureAPIRejection(t, err)
		return "CHUNK_RECORD_COUNT_LIMIT"
	case "outlinks_empty":
		_, err := NewOutlinksStageChunk(commitID, 0, profile.context, []string{})
		requireFixtureAPIRejection(t, err)
		return "CHUNK_RECORD_COUNT_LIMIT"
	case "discoveries_65_records":
		values := make([]OutputDiscovery, MaxNonBlobStageBatchRecords+1)
		for index := range values {
			values[index] = profile.output.Discoveries[0]
		}
		_, err := NewDiscoveriesStageChunk(commitID, 0, values)
		requireFixtureAPIRejection(t, err)
		return "CHUNK_RECORD_COUNT_LIMIT"
	case "aliases_duplicate":
		alias := cloneRecord(profile.sections.Aliases[0])
		_, err := newValidatedStageChunk(commitID, ChunkAliases, 0, []Record{alias, cloneRecord(alias)})
		requireFixtureAPIRejection(t, err)
		if !errors.Is(err, ErrDuplicateChunkRecord) {
			t.Fatalf("duplicate alias chunk error = %v", err)
		}
		return "DUPLICATE_CHUNK_RECORD"
	case "image_manifest_one_over":
		record := Record{
			textField("contract_version", "1"),
			textField("publication_id", profile.result.PublicationID),
			textField("normalized_url", profile.output.Page.NormalizedURL),
			textField("image_count", "0"),
			textField("image_keys", "["+strings.Repeat(" ", MaxImageManifestBytes)+"]"),
		}
		_, err := newValidatedStageChunk(commitID, ChunkImageManifest, 0, []Record{record})
		requireFixtureAPIRejection(t, err)
		return "IMAGE_MANIFEST_LIMIT"
	case "blob_one_over":
		_, err := NewPageBlobStageChunk(commitID, ChunkHTML, bytes.Repeat([]byte{'H'}, MaxPageBlobBytes+1))
		requireFixtureAPIRejection(t, err)
		return "PAGE_BLOB_LIMIT"
	default:
		t.Fatalf("unsupported stage mutation %q", mutation)
	}
	return ""
}

func fixtureTypeRejection(t testing.TB, field string, raw json.RawMessage) string {
	t.Helper()
	switch field {
	case "rendered":
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil {
			return "FIXTURE_TYPE"
		}
	case "fence", "status_code":
		var value uint64
		if err := json.Unmarshal(raw, &value); err != nil {
			return "FIXTURE_TYPE"
		}
	default:
		t.Fatalf("unsupported fixture type field %q", field)
	}
	return ""
}

var errFixtureDuplicateJSONKey = errors.New("duplicate JSON key")

func strictJSONRejection(raw []byte) string {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := consumeStrictJSONValue(decoder); err != nil {
		if errors.Is(err, errFixtureDuplicateJSONKey) {
			return "DUPLICATE_JSON_KEY"
		}
		return "INVALID_JSON"
	}
	if _, err := decoder.Token(); err != io.EOF {
		return "INVALID_JSON"
	}
	return ""
}

func consumeStrictJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not text")
			}
			if _, duplicate := seen[key]; duplicate {
				return errFixtureDuplicateJSONKey
			}
			seen[key] = struct{}{}
			if err := consumeStrictJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return errors.New("invalid object close")
		}
	case '[':
		for decoder.More() {
			if err := consumeStrictJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("invalid array close")
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	return nil
}

var fixtureCanonicalDecimalPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]*)$`)

func fixtureU64Rejection(value string) string {
	if !fixtureCanonicalDecimalPattern.MatchString(value) {
		return "INVALID_UNSIGNED_DECIMAL"
	}
	if _, err := strconv.ParseUint(value, 10, 64); err != nil {
		return "U64_RANGE"
	}
	return ""
}

func (h *fixtureConformanceHarness) fixtureOutputUTF8Rejection(t *testing.T, field, bytesHex string) string {
	t.Helper()
	raw, err := hex.DecodeString(bytesHex)
	if err != nil {
		t.Fatalf("decode fixture bytes: %v", err)
	}
	if utf8.Valid(raw) {
		t.Fatal("negative UTF-8 fixture contains valid UTF-8")
	}
	context := vectorOutputContextValue(t, h.fixture)
	output := cloneFixtureOutput(vectorOutputValue(t, h.fixture))
	switch field {
	case "html":
		output.Page.HTML = raw
	case "original_html":
		output.Page.OriginalHTML = raw
	case "alt":
		output.Images[0].Alt = string(raw)
	case "url":
		output.Outlinks = []string{string(raw)}
	case "content_type":
		output.Page.ContentType = string(raw)
	case "render_policy_rule":
		context = vectorOutputContextWith(t, h.fixture, len(h.fixture.OutputContext.Requests), []string{"render-main"})
		output.Page.Rendered = true
		output.Page.OriginalHTML = []byte("source")
		output.Page.RenderPolicyRule = string(raw)
		output.Page.RenderPolicyDigest = mustFixtureDigest(t, h.fixture.Identities.CrawlPolicyDigest)
	default:
		t.Fatalf("unsupported UTF-8 field %q", field)
	}
	_, err = DeriveOutputDigest(context, output)
	requireFixtureAPIRejection(t, err)
	return "INVALID_UTF8"
}

func (h *fixtureConformanceHarness) fixtureTransitionReplayRejection(t *testing.T, mutation string) string {
	t.Helper()
	lease := vectorLease(t, h.fixture)
	base, err := DeriveRetryTransitionID(RetryTransitionInput{Lease: lease, Reason: ReasonRequestTimeout})
	if err != nil {
		t.Fatal(err)
	}
	var submitted Digest
	switch mutation {
	case "legal_reason":
		submitted, err = DeriveRetryTransitionID(RetryTransitionInput{Lease: lease, Reason: ReasonDNSTemporary})
	case "semantic_payload":
		lease.OwnerID = mustOwnerID(t, h.fixture.Identities.AlternateOwnerID)
		submitted, err = DeriveRetryTransitionID(RetryTransitionInput{Lease: lease, Reason: ReasonRequestTimeout})
	default:
		t.Fatalf("unsupported transition replay mutation %q", mutation)
	}
	if err != nil {
		t.Fatal(err)
	}
	return fixtureReplayRejection(base, submitted)
}
