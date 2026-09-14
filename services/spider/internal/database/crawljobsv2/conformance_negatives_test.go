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
	case "transcript_binding_mutation":
		return runTranscriptBindingNegativeCase(t, h.fixture, vector)
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
		context := vectorOutputContextValue(t, h.fixture)
		_, chunkErr := NewDiscoveriesStageChunk(outputCommitIdentity(context, mustFixtureDigest(t, h.fixture.Expected.PublicationID)), 0, context, output.Discoveries)
		requireFixtureAPIRejection(t, chunkErr)
		return "POLICY_BINDING_MISMATCH"
	case "output_normalized_target":
		input := decodeVectorPart[struct {
			Value string `json:"value"`
		}](t, vector.Input, vector.Name+".input")
		output := vectorOutputValue(t, h.fixture)
		output.Page.NormalizedURL = vectorTargetValue(t, h.fixture, input.Value).CanonicalURL
		_, err := DeriveOutputDigest(vectorOutputContextValue(t, h.fixture), output)
		return fixtureProductionOutputRejectionClass(t, err)
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
	case "policy_group_boundary":
		validateFixturePolicyGroupGeneratorShape(t, vector.Input, vector.Name+".input")
		input := decodeVectorPart[fixturePolicyGroupGenerator](t, vector.Input, vector.Name+".input")
		groups := fixturePolicyGroupsFromGenerator(t, input)
		_, err := DerivePolicyGroupMapDigest(groups)
		if !errors.Is(err, ErrInputLimitExceeded) {
			t.Fatalf("policy-group count boundary error = %v", err)
		}
		return "POLICY_GROUP_COUNT_LIMIT"
	case "group_id":
		input := decodeVectorPart[struct {
			Value string `json:"value"`
		}](t, vector.Input, vector.Name+".input")
		if _, err := ParseGroupID(input.Value); !errors.Is(err, ErrInvalidGroupID) {
			t.Fatalf("group-ID control-character error = %v", err)
		}
		return "INVALID_GROUP_ID"
	case "policy_group_binding":
		input := decodeVectorPart[struct {
			Mutation string `json:"mutation"`
		}](t, vector.Input, vector.Name+".input")
		return h.fixturePolicyGroupBindingRejection(t, input.Mutation)
	case "guard_core_mutation":
		input := decodeVectorPart[struct {
			BaseCase string `json:"base_case"`
			Mutation string `json:"mutation"`
		}](t, vector.Input, vector.Name+".input")
		return h.fixtureGuardCoreMutationRejection(t, input.BaseCase, input.Mutation)
	case "guard_chain_mutation":
		input := decodeVectorPart[struct {
			BaseCase string `json:"base_case"`
			Mutation string `json:"mutation"`
		}](t, vector.Input, vector.Name+".input")
		return h.fixtureGuardChainMutationRejection(t, input.BaseCase, input.Mutation)
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
		authority := vectorRunPolicyAuthority(t, h.fixture, plainSHA256(testDenyAllRenderPolicyArtifact()))
		_, err := DeriveReservationID(authority, intent)
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
		jobs := fixtureCountSourceJobs(t, vectorSourceJobValue(t, h.fixture, 0), input.Count)
		_, err := DeriveSourceDigest(jobs)
		if err == nil {
			return ""
		}
		// This is the non-authoritative pre-run source formula, not enqueue
		// admission. A malformed job or a broader error is not count evidence.
		if err != ErrInputLimitExceeded {
			t.Fatalf("source count error = %v; want exact ErrInputLimitExceeded", err)
		}
		return "SOURCE_COUNT_LIMIT"
	case "section_shape":
		input := decodeVectorPart[struct {
			Section string `json:"section"`
			Count   int    `json:"count"`
		}](t, vector.Input, vector.Name+".input")
		return fixtureSectionShapeRejection(t, h.fixture, input.Section, input.Count)
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

func (h *fixtureConformanceHarness) fixturePolicyGroupBindingRejection(t *testing.T, mutation string) string {
	t.Helper()
	groups := vectorPolicyGroupsValue(t, h.fixture)
	authority := vectorRunPolicyAuthority(t, h.fixture, plainSHA256(testDenyAllRenderPolicyArtifact()))
	decision := vectorDecisionValue(t, h.fixture, "page_document")
	bindings := RunPinnedPolicyBindings{Decisions: []PolicyDecision{decision}}
	switch mutation {
	case "missing_group":
		filtered := make([]PolicyGroup, 0, len(groups)-1)
		for _, group := range groups {
			if group.GroupID != decision.GroupID {
				filtered = append(filtered, group)
			}
		}
		authority = newAuthenticatedTestRunPolicyAuthority(
			t, vectorLease(t, h.fixture).RunID, mustFixtureDigest(t, h.fixture.Identities.CrawlPolicyDigest),
			plainSHA256(testDenyAllRenderPolicyArtifact()), filtered,
		)
	case "wrong_rate_lineage":
		var alternate PolicyGroup
		for _, group := range groups {
			if group.GroupID != decision.GroupID {
				alternate = group
				break
			}
		}
		if alternate.GroupID == "" {
			t.Fatal("fixture has no alternate policy rate lineage")
		}
		source := vectorSourceJobValue(t, h.fixture, 0)
		source.RateScopeID = alternate.RateScopeID
		source.Decision.RateScopeID = alternate.RateScopeID
		source.Decision.GroupScopeID = alternate.GroupScopeID
		bindings.Decisions = nil
		bindings.SourceJobs = []SourceJob{source}
	case "changed_group_tuple":
		for index := range groups {
			if groups[index].GroupID == decision.GroupID {
				groups[index].Concurrency--
			}
		}
		authority = newAuthenticatedTestRunPolicyAuthority(
			t, vectorLease(t, h.fixture).RunID, mustFixtureDigest(t, h.fixture.Identities.CrawlPolicyDigest),
			plainSHA256(testDenyAllRenderPolicyArtifact()), groups,
		)
		bindings.Decisions = nil
		bindings.Discoveries = []OutputDiscovery{vectorOutputValue(t, h.fixture).Discoveries[0]}
	case "unequal_group_origin_tuple":
		decision.OriginConcurrency--
		bindings.Decisions = []PolicyDecision{decision}
	case "unrelated_group_map":
		unrelatedID, err := ParseGroupID("unrelated-policy-group")
		if err != nil {
			t.Fatal(err)
		}
		unrelatedRate, err := ParseRateScopeID(strings.Repeat("9", 32))
		if err != nil {
			t.Fatal(err)
		}
		unrelatedScope, err := DeriveGroupScopeID(unrelatedRate)
		if err != nil {
			t.Fatal(err)
		}
		unrelatedGroups := []PolicyGroup{{
			GroupID: unrelatedID, RateScopeID: unrelatedRate, GroupScopeID: unrelatedScope,
			RequestStartLimit: 1, Concurrency: 1, IntervalMS: 0,
		}}
		unrelatedAuthority := newAuthenticatedTestRunPolicyAuthority(
			t, vectorLease(t, h.fixture).RunID, mustFixtureDigest(t, h.fixture.Identities.CrawlPolicyDigest),
			plainSHA256(testDenyAllRenderPolicyArtifact()), unrelatedGroups,
		)
		surfaces := []RunPinnedPolicyBindings{
			{Decisions: []PolicyDecision{decision}},
			{SourceJobs: []SourceJob{vectorSourceJobValue(t, h.fixture, 0)}},
			{Discoveries: []OutputDiscovery{vectorOutputValue(t, h.fixture).Discoveries[0]}},
		}
		for index, surface := range surfaces {
			if err := ValidateRunPinnedPolicyBindings(unrelatedAuthority, surface); !errors.Is(err, ErrPolicyGroupBindingMismatch) {
				t.Fatalf("unrelated group map surface %d error = %v", index, err)
			}
		}
		return "POLICY_GROUP_BINDING_MISMATCH"
	default:
		t.Fatalf("unsupported policy-group binding mutation %q", mutation)
	}
	err := ValidateRunPinnedPolicyBindings(authority, bindings)
	if !errors.Is(err, ErrPolicyGroupBindingMismatch) {
		t.Fatalf("policy-group binding production error = %v", err)
	}
	return "POLICY_GROUP_BINDING_MISMATCH"
}

func (h *fixtureConformanceHarness) fixtureGuardCoreMutationRejection(t *testing.T, baseCase, mutation string) string {
	t.Helper()
	mode, input := h.fixtureGuardCoreInputByCase(t, baseCase)
	switch mutation {
	case "zero_contract":
		input.ContractSHA256 = Digest(ZeroSHA256)
	case "zero_redis_config":
		input.RedisConfigSHA256 = Digest(ZeroSHA256)
	case "all_nonzero_provisional":
		input.MaximumShapeSHA256 = Digest(strings.Repeat("2", 64))
		input.MemoryFixtureSHA256 = Digest(strings.Repeat("3", 64))
		input.LuaBenchmarkSHA256 = Digest(strings.Repeat("4", 64))
		input.AOFCrashEvidenceSHA256 = Digest(strings.Repeat("5", 64))
	case "migration_provisional":
		input.CutoverMode = CutoverV1Migration
		input.CandidateRunID = RunID(strings.Repeat("a", 32))
	case "production_zero_evidence":
		input.MaximumShapeSHA256 = Digest(ZeroSHA256)
	case "redis_6":
		input.RedisVersion = "6.2.0"
	case "redis_8":
		input.RedisVersion = "8.0.0"
	default:
		t.Fatalf("unsupported guard-core mutation %q", mutation)
	}
	var err error
	switch mode {
	case "production":
		_, err = NewGuardCore(input)
	case "provisional_fixture":
		_, err = NewProvisionalGuardCore(input)
	default:
		t.Fatalf("unsupported fixture guard mode %q", mode)
	}
	if !errors.Is(err, ErrInvalidRecordValue) {
		t.Fatalf("guard-core production error = %v", err)
	}
	if mutation == "redis_6" || mutation == "redis_8" {
		return "INVALID_REDIS_VERSION"
	}
	return "INVALID_GUARD_RELATION"
}

func (h *fixtureConformanceHarness) fixtureGuardChainMutationRejection(t *testing.T, baseCase, mutation string) string {
	t.Helper()
	vector := h.fixtureCaseByName(t, baseCase)
	if vector.Kind != "guard_chain" {
		t.Fatalf("guard-chain mutation base %q has kind %q", baseCase, vector.Kind)
	}
	input := decodeVectorPart[fixtureGuardChainInput](t, vector.Input, baseCase+".input")
	contractDigest := h.fixtureContractDigest(t, input.ContractCase)
	core, err := NewGuardCore(fixtureGuardCoreValue(t, contractDigest, input.GuardCore))
	if err != nil {
		t.Fatal(err)
	}
	coreDigest, err := core.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	switch mutation {
	case "redis_config_disagreement":
		input.Compatibility.RedisConfigSHA256 = strings.Repeat("f", 64)
	case "zero_image_digest":
		input.Compatibility.SpiderImage = "sha256:" + ZeroSHA256
	default:
		t.Fatalf("unsupported guard-chain mutation %q", mutation)
	}
	artifact, err := NewCompatibilityArtifact(CompatibilityArtifactInput{
		RedisConfigSHA256:       mustFixtureDigest(t, input.Compatibility.RedisConfigSHA256),
		CommitGuardSHA256:       coreDigest,
		SpiderImage:             mustImageDigest(t, input.Compatibility.SpiderImage),
		SeedImporterImage:       mustImageDigest(t, input.Compatibility.SeedImporterImage),
		CrawlAdminImage:         mustImageDigest(t, input.Compatibility.CrawlAdminImage),
		IndexerImage:            mustImageDigest(t, input.Compatibility.IndexerImage),
		ImageIndexerImage:       mustImageDigest(t, input.Compatibility.ImageIndexerImage),
		BacklinksProcessorImage: mustImageDigest(t, input.Compatibility.BacklinksProcessorImage),
		MonitoringImage:         mustImageDigest(t, input.Compatibility.MonitoringImage),
		RenderWorkerImage:       input.Compatibility.RenderWorkerImage,
	})
	if mutation == "zero_image_digest" {
		if !errors.Is(err, ErrInvalidRecordValue) {
			t.Fatalf("zero image digest production error = %v", err)
		}
		return "INVALID_IMAGE_DIGEST"
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateGuardCompatibility(core, artifact); !errors.Is(err, ErrArtifactMismatch) {
		t.Fatalf("guard/config disagreement production error = %v", err)
	}
	return "GUARD_CONFIG_MISMATCH"
}

func (h *fixtureConformanceHarness) fixtureGuardCoreInputByCase(t *testing.T, name string) (string, GuardCoreInput) {
	t.Helper()
	vector := h.fixtureCaseByName(t, name)
	var mode, contractCase string
	var spec fixtureGuardCoreSpec
	switch vector.Kind {
	case "guard_core":
		input := decodeVectorPart[fixtureGuardCoreCaseInput](t, vector.Input, name+".input")
		mode, contractCase, spec = input.GuardMode, input.ContractCase, input.GuardCore
	case "guard_chain":
		input := decodeVectorPart[fixtureGuardChainInput](t, vector.Input, name+".input")
		mode, contractCase, spec = input.GuardMode, input.ContractCase, input.GuardCore
	default:
		t.Fatalf("guard mutation base %q has kind %q", name, vector.Kind)
	}
	return mode, fixtureGuardCoreValue(t, h.fixtureContractDigest(t, contractCase), spec)
}

func (h *fixtureConformanceHarness) fixtureCaseByName(t testing.TB, name string) digestVectorCase {
	t.Helper()
	for _, vector := range h.fixture.Cases {
		if vector.Name == name {
			return vector
		}
	}
	t.Fatalf("unknown positive fixture case %q", name)
	return digestVectorCase{}
}

func (h *fixtureConformanceHarness) fixtureContractDigest(t testing.TB, name string) Digest {
	t.Helper()
	vector := h.fixtureCaseByName(t, name)
	if vector.Kind != "contract_digest" {
		t.Fatalf("guard contract case %q has kind %q", name, vector.Kind)
	}
	expected := decodeVectorPart[contractCaseResult](t, vector.Expected, name+".expected")
	return mustFixtureDigest(t, expected.ContractSHA256)
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
	case "render_true_without_original", "render_true_without_rule", "render_true_rule_one_over", "render_true_rule_control", "render_true_bad_digest":
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
		case "render_true_rule_one_over":
			output.Page.RenderPolicyRule = strings.Repeat("r", MaxRenderPolicyRuleIDBytes+1)
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
		return fixtureProductionOutputRejectionClass(t, err)
	default:
		t.Fatalf("unsupported output mutation %q", mutation)
	}
	_, err := DeriveOutputDigest(context, output)
	return fixtureProductionOutputRejectionClass(t, err)
}

func fixtureProductionOutputRejectionClass(t testing.TB, err error) string {
	t.Helper()
	requireFixtureAPIRejection(t, err)
	if class, ok := OutputRejectionClass(err); ok {
		return class
	}
	if errors.Is(err, ErrURLIdentityMismatch) {
		return "URL_IDENTITY_MISMATCH"
	}
	t.Fatalf("production error has no stable output rejection class: %v", err)
	return ""
}

func TestOutputRejectionSentinelsAreStableAndBackwardCompatible(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		class         string
		compatibility []error
	}{
		{"duplicate_outlink", ErrDuplicateOutlink, "DUPLICATE_OUTLINK", nil},
		{"duplicate_image", ErrDuplicateImage, "DUPLICATE_IMAGE", nil},
		{"duplicate_discovery", ErrDuplicateDiscovery, "DUPLICATE_DISCOVERY", nil},
		{"duplicate_alias", ErrDuplicateAlias, "DUPLICATE_ALIAS", nil},
		{"count_limit", ErrOutputCountLimit, "OUTPUT_COUNT_LIMIT", []error{ErrInputLimitExceeded}},
		{"alias_count_limit", ErrOutputAliasCountLimit, "ALIAS_COUNT_LIMIT", []error{ErrInputLimitExceeded}},
		{"page_blob_limit", ErrOutputPageBlobLimit, "PAGE_BLOB_LIMIT", []error{ErrInvalidOutput}},
		{"combined_html_limit", ErrOutputCombinedHTMLLimit, "COMBINED_HTML_LIMIT", []error{ErrInvalidOutput}},
		{"image_alt_limit", ErrOutputImageAltLimit, "IMAGE_ALT_LIMIT", []error{ErrInvalidOutput}},
		{"content_type_limit", ErrOutputContentTypeLimit, "CONTENT_TYPE_LIMIT", []error{ErrInvalidOutput}},
		{"invalid_content_type", ErrOutputInvalidContentType, "INVALID_CONTENT_TYPE", []error{ErrInvalidOutput}},
		{"status_code_range", ErrOutputStatusCodeRange, "STATUS_CODE_RANGE", []error{ErrInvalidOutput}},
		{"invalid_render_relation", ErrOutputInvalidRenderRelation, "INVALID_RENDER_RELATION", []error{ErrInvalidOutput}},
		{"render_rule_limit", ErrOutputRenderRuleLimit, "RENDER_RULE_LIMIT", []error{ErrInvalidOutput}},
		{"invalid_render_policy_digest", ErrOutputInvalidRenderPolicyDigest, "INVALID_RENDER_POLICY_DIGEST", []error{ErrDigestInputMismatch}},
		{"invalid_utf8", ErrOutputInvalidUTF8, "INVALID_UTF8", []error{ErrInvalidOutput, ErrInvalidCanonicalURL}},
		{"url_too_long", ErrOutputURLTooLong, "URL_TOO_LONG", []error{ErrInvalidCanonicalURL}},
		{"group_id_limit", ErrOutputGroupIDLimit, "GROUP_ID_LIMIT", []error{ErrInvalidGroupID}},
		{"context_mismatch", ErrOutputContextMismatch, "OUTPUT_CONTEXT_MISMATCH", []error{ErrInvalidOutput, ErrDigestInputMismatch}},
	}
	seenClasses := make(map[string]struct{}, len(tests))
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			class, ok := OutputRejectionClass(test.err)
			if !ok || class != test.class {
				t.Fatalf("production rejection class = %q, %t; want %q, true", class, ok, test.class)
			}
			if !errors.Is(test.err, test.err) {
				t.Fatal("static sentinel does not match itself")
			}
			for _, broader := range test.compatibility {
				if !errors.Is(test.err, broader) {
					t.Fatalf("%v does not preserve errors.Is compatibility with %v", test.err, broader)
				}
			}
			if strings.Contains(test.err.Error(), "output-secret-canary") {
				t.Fatal("static rejection error exposed caller-controlled input")
			}
		})
		if _, duplicate := seenClasses[test.class]; duplicate {
			t.Fatalf("duplicate production output rejection class %q", test.class)
		}
		seenClasses[test.class] = struct{}{}
	}
}

func TestOutputRejectionClassDetectsChangedProductionReturn(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	context := vectorOutputContextValue(t, fixture)
	tests := []struct {
		name          string
		mutate        func(*CrawlOutput)
		substitute    error
		expectedClass string
	}{
		{
			name: "duplicate",
			mutate: func(output *CrawlOutput) {
				output.Outlinks = append(output.Outlinks, output.Outlinks[0])
			},
			substitute: ErrDuplicateImage, expectedClass: "DUPLICATE_OUTLINK",
		},
		{
			name: "limit",
			mutate: func(output *CrawlOutput) {
				output.Outlinks = make([]string, MaxOutlinksPerJob+1)
			},
			substitute: ErrOutputPageBlobLimit, expectedClass: "OUTPUT_COUNT_LIMIT",
		},
		{
			name: "error",
			mutate: func(output *CrawlOutput) {
				output.Page.ContentType = ""
			},
			substitute: ErrOutputStatusCodeRange, expectedClass: "INVALID_CONTENT_TYPE",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := cloneFixtureOutput(vectorOutputValue(t, fixture))
			test.mutate(&output)
			_, productionErr := DeriveOutputDigest(context, output)
			class, ok := OutputRejectionClass(productionErr)
			if !ok || class != test.expectedClass {
				t.Fatalf("expected production return class = %q, %t; want %q, true", class, ok, test.expectedClass)
			}
			changedClass, ok := OutputRejectionClass(test.substitute)
			if !ok {
				t.Fatal("substituted production return lost its typed class")
			}
			if changedClass == test.expectedClass {
				t.Fatalf("changing the production %s return preserved the incorrect expected class %q", test.name, changedClass)
			}
		})
	}
}

func fmtURL(host string, index int) string {
	return "https://" + host + "/" + fmt.Sprintf("%03d", index)
}

func fixtureSectionShapeRejection(t *testing.T, fixture digestVectorFixture, section string, count int) string {
	t.Helper()
	if count < 0 || count > MaxJobsPerRun+1 {
		t.Fatal("unsupported fixture section count")
	}
	if section == "page" {
		// Grammar-only evidence: CrawlOutput has one Page value, not a page
		// slice. EncodeSection is framing, not a semantic section decoder.
		t.Log("grammar-only page-section cardinality check; no production API consumes this arbitrary page-count grammar")
		if count != 1 {
			return "PAGE_SECTION_SHAPE"
		}
		return ""
	}
	if section == "aliases" {
		aliases := make([]outputAlias, count)
		for index := range aliases {
			target := mustFixtureTargetForURL(t, fmtURL("count-aliases.example.com", index))
			aliases[index] = outputAlias{URLID: target.URLID, CanonicalURL: target.CanonicalURL, Depth: 2}
		}
		// The production semantic encoder consumes an alias slice; this is
		// schema evidence, not authority to construct an arbitrary OutputContext.
		_, err := outputAliasRecords(aliases)
		if err == nil {
			return ""
		}
		if err != ErrOutputAliasCountLimit {
			t.Fatalf("alias count error = %v; want exact ErrOutputAliasCountLimit", err)
		}
		return fixtureProductionOutputRejectionClass(t, err)
	}
	context := vectorOutputContextValue(t, fixture)
	output := vectorOutputValue(t, fixture)
	switch section {
	case "outlinks":
		output.Outlinks = make([]string, count)
		for index := range output.Outlinks {
			output.Outlinks[index] = fmtURL("count-outlinks.example.com", index)
		}
	case "images":
		output.Images = make([]OutputImage, count)
		for index := range output.Images {
			output.Images[index] = OutputImage{NormalizedSourceURL: fmtURL("count-images.example.com", index), Alt: "count fixture"}
		}
	case "discoveries":
		prototype := output.Discoveries[0]
		jobs := fixtureCountSourceJobs(t, SourceJob{
			JobID: prototype.JobID, CanonicalURL: prototype.CanonicalURL, ScoreText: prototype.ScoreText,
			Depth: prototype.Depth, GroupID: prototype.GroupID, RateScopeID: prototype.RateScopeID, Decision: prototype.Decision,
		}, count)
		output.Discoveries = make([]OutputDiscovery, count)
		for index, job := range jobs {
			output.Discoveries[index] = OutputDiscovery{
				JobID: job.JobID, CanonicalURL: job.CanonicalURL, ScoreText: job.ScoreText,
				Depth: job.Depth, GroupID: job.GroupID, RateScopeID: job.RateScopeID, Decision: job.Decision,
			}
		}
	default:
		t.Fatalf("unsupported output section %q", section)
	}
	_, err := DeriveOutputDigest(context, output)
	if err == nil {
		return ""
	}
	if err != ErrOutputCountLimit {
		t.Fatalf("%s count error = %v; want exact ErrOutputCountLimit", section, err)
	}
	return fixtureProductionOutputRejectionClass(t, err)
}

func (h *fixtureConformanceHarness) fixtureStageMutationRejection(t *testing.T, mutation string) string {
	t.Helper()
	profile := h.outputProfile(t, "baseline")
	commitID := mustFixtureDigest(t, profile.result.CommitID)
	identity := outputCommitIdentity(profile.context, mustFixtureDigest(t, profile.result.PublicationID))
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
		_, err := NewOutlinksStageChunk(identity, 0, profile.context, values)
		if err != ErrInvalidChunk {
			t.Fatalf("65 valid outlinks error = %v; want exact ErrInvalidChunk", err)
		}
		return "CHUNK_RECORD_COUNT_LIMIT"
	case "outlinks_empty":
		_, err := NewOutlinksStageChunk(identity, 0, profile.context, []string{})
		if err != ErrInvalidChunk {
			t.Fatalf("empty outlink chunk error = %v; want exact ErrInvalidChunk", err)
		}
		return "CHUNK_RECORD_COUNT_LIMIT"
	case "discoveries_65_records":
		values := fixtureCountDiscoveries(t, profile.context, profile.output.Discoveries[0], MaxNonBlobStageBatchRecords+1)
		_, err := NewDiscoveriesStageChunk(identity, 0, profile.context, values)
		if err != ErrInputLimitExceeded {
			t.Fatalf("65 valid unique discoveries error = %v; want exact ErrInputLimitExceeded", err)
		}
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
		record := fixtureSizedImageManifestRecord(t, identity.PublicationID, profile.output.Page.NormalizedURL, MaxImageManifestBytes+2)
		_, err := newValidatedStageChunk(commitID, ChunkImageManifest, 0, []Record{record})
		if err != ErrInvalidChunk {
			t.Fatalf("oversized compact image manifest error = %v; want exact ErrInvalidChunk, not a source-URL content error", err)
		}
		return "IMAGE_MANIFEST_LIMIT"
	case "blob_one_over":
		_, err := NewPageBlobStageChunk(identity, profile.context, ChunkHTML, bytes.Repeat([]byte{'H'}, MaxPageBlobBytes+1))
		if err != ErrInvalidChunk {
			t.Fatalf("oversized valid UTF-8 blob error = %v; want exact ErrInvalidChunk", err)
		}
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
	return fixtureProductionOutputRejectionClass(t, err)
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
