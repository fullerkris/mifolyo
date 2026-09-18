package crawljobsv2

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

type fixtureConformanceHarness struct {
	fixture                 digestVectorFixture
	contractCases           map[string]contractCaseResult
	outputProfiles          map[string]*fixtureOutputProfile
	sourceProfiles          map[string]sourceProfileResult
	sourceProfileGenerators map[string]string
}

func newFixtureConformanceHarness(t *testing.T, fixture digestVectorFixture) *fixtureConformanceHarness {
	t.Helper()
	return &fixtureConformanceHarness{
		fixture:                 fixture,
		contractCases:           make(map[string]contractCaseResult),
		outputProfiles:          make(map[string]*fixtureOutputProfile),
		sourceProfiles:          make(map[string]sourceProfileResult),
		sourceProfileGenerators: make(map[string]string),
	}
}

func TestSharedFixtureV2PositiveCases(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	harness := newFixtureConformanceHarness(t, fixture)
	consumed := make(map[string]struct{}, len(fixture.Cases))

	for _, vector := range fixture.Cases {
		vector := vector
		t.Run(vector.Name, func(t *testing.T) {
			switch vector.Kind {
			case "u64_max":
				verifyU64MaximumCase(t, vector)
			case "empty_section":
				verifyEmptySectionCase(t, vector)
			case "valid_score":
				verifyValidScoreCase(t, vector)
			case "utf8_order":
				verifyUTF8OrderCase(t, vector)
			case "contract_digest":
				result := verifyContractDigestCase(t, vector)
				harness.contractCases[vector.Name] = result
			case "canonical_lua_bundle":
				result := verifyCanonicalLuaBundleCase(t, vector)
				harness.contractCases[vector.Name] = contractCaseResult{result.LuaSourceOrder, result.ContractSHA256}
			case "policy_group_boundary":
				verifyPolicyGroupBoundaryCase(t, vector)
			case "guard_chain":
				harness.verifyGuardChainCase(t, vector)
			case "guard_core":
				harness.verifyGuardCoreCase(t, vector)
			case "transition_mutation":
				harness.verifyTransitionMutationCase(t, vector)
			case "publication_independence":
				harness.verifyPublicationIndependenceCase(t, vector)
			case "output_profile":
				harness.verifyOutputProfileCase(t, vector)
			case "source_profile":
				harness.verifySourceProfileCase(t, vector)
			case "stage_chunks":
				harness.verifyStageChunksCase(t, vector)
			case "field_limits":
				harness.verifyFieldLimitsCase(t, vector)
			case "transcript_binding":
				verifyTranscriptBindingCase(t, fixture, vector)
			default:
				t.Fatalf("unsupported positive fixture kind %q", vector.Kind)
			}
		})
		consumed[vector.Name] = struct{}{}
	}

	if len(consumed) != len(fixture.Cases) {
		t.Fatalf("consumed %d of %d positive fixture cases", len(consumed), len(fixture.Cases))
	}
}

func verifyU64MaximumCase(t *testing.T, vector digestVectorCase) {
	t.Helper()
	input := decodeVectorPart[struct {
		Decimal string `json:"decimal"`
	}](t, vector.Input, vector.Name+".input")
	expected := decodeVectorPart[struct {
		U64Hex         string `json:"u64_hex"`
		FrameLengthHex string `json:"frame_length_hex"`
	}](t, vector.Expected, vector.Name+".expected")
	value, err := strconv.ParseUint(input.Decimal, 10, 64)
	if err != nil || strconv.FormatUint(value, 10) != input.Decimal {
		t.Fatalf("parse maximum U64 %q: %v", input.Decimal, err)
	}
	encoded := hex.EncodeToString(U64(value))
	actual := struct {
		U64Hex         string `json:"u64_hex"`
		FrameLengthHex string `json:"frame_length_hex"`
	}{U64Hex: encoded, FrameLengthHex: encoded}
	assertVectorValue(t, actual, expected)
}

func verifyEmptySectionCase(t *testing.T, vector digestVectorCase) {
	t.Helper()
	input := decodeVectorPart[struct {
		Label string `json:"label"`
	}](t, vector.Input, vector.Name+".input")
	expected := decodeVectorPart[struct {
		SectionHex    string `json:"section_hex"`
		SectionSHA256 string `json:"section_sha256"`
	}](t, vector.Expected, vector.Name+".expected")
	encoded, err := EncodeSection(input.Label, []Record{})
	if err != nil {
		t.Fatalf("encode empty section %q: %v", input.Label, err)
	}
	actual := struct {
		SectionHex    string `json:"section_hex"`
		SectionSHA256 string `json:"section_sha256"`
	}{hex.EncodeToString(encoded), fixtureSHA256(encoded)}
	assertVectorValue(t, actual, expected)
}

func verifyValidScoreCase(t *testing.T, vector digestVectorCase) {
	t.Helper()
	input := decodeVectorPart[struct {
		ScoreText  string `json:"score_text"`
		RedisValue string `json:"redis_value"`
	}](t, vector.Input, vector.Name+".input")
	expected := decodeVectorPart[struct {
		Binary64Hex string `json:"binary64_hex"`
	}](t, vector.Expected, vector.Name+".expected")
	score, err := ParseScoreText(input.ScoreText)
	if err != nil {
		t.Fatalf("parse canonical score: %v", err)
	}
	if err := ValidateRedisScore(score, input.RedisValue); err != nil {
		t.Fatalf("validate equivalent Redis score: %v", err)
	}
	value, err := score.Float64()
	if err != nil {
		t.Fatal(err)
	}
	encoded := make([]byte, 8)
	binary.BigEndian.PutUint64(encoded, math.Float64bits(value))
	actual := struct {
		Binary64Hex string `json:"binary64_hex"`
	}{hex.EncodeToString(encoded)}
	assertVectorValue(t, actual, expected)
}

func verifyUTF8OrderCase(t *testing.T, vector digestVectorCase) {
	t.Helper()
	input := decodeVectorPart[struct {
		Values []string `json:"values"`
	}](t, vector.Input, vector.Name+".input")
	expected := decodeVectorPart[struct {
		OrderedValues []string `json:"ordered_values"`
		SectionSHA256 string   `json:"section_sha256"`
	}](t, vector.Expected, vector.Name+".expected")
	ordered := append([]string(nil), input.Values...)
	sort.Slice(ordered, func(left, right int) bool {
		return bytes.Compare([]byte(ordered[left]), []byte(ordered[right])) < 0
	})
	records := make([]Record, len(ordered))
	for index, value := range ordered {
		records[index] = Record{textField("value", value)}
	}
	encoded, err := EncodeSection("values", records)
	if err != nil {
		t.Fatal(err)
	}
	actual := struct {
		OrderedValues []string `json:"ordered_values"`
		SectionSHA256 string   `json:"section_sha256"`
	}{ordered, fixtureSHA256(encoded)}
	assertVectorValue(t, actual, expected)
}

type contractCaseResult struct {
	LuaSourceOrder []string `json:"lua_source_order"`
	ContractSHA256 string   `json:"contract_sha256"`
}

type fixturePolicyGroupGenerator struct {
	Grammar              string `json:"grammar"`
	Count                int    `json:"count"`
	FirstIndex           int    `json:"first_index"`
	IndexWidth           int    `json:"index_width"`
	IndexRadix           int    `json:"index_radix"`
	GroupIDTemplate      string `json:"group_id_template"`
	RateScopeIDTemplate  string `json:"rate_scope_id_template"`
	RequestStartLimit    uint64 `json:"request_start_limit"`
	Concurrency          uint64 `json:"concurrency"`
	IntervalMilliseconds uint64 `json:"interval_ms"`
}

func verifyPolicyGroupBoundaryCase(t *testing.T, vector digestVectorCase) {
	t.Helper()
	validateFixturePolicyGroupGeneratorShape(t, vector.Input, vector.Name+".input")
	input := decodeVectorPart[fixturePolicyGroupGenerator](t, vector.Input, vector.Name+".input")
	expected := decodeVectorPart[struct {
		Count  int    `json:"count"`
		SHA256 string `json:"policy_group_map_sha256"`
	}](t, vector.Expected, vector.Name+".expected")
	groups := fixturePolicyGroupsFromGenerator(t, input)
	digest, err := DerivePolicyGroupMapDigest(groups)
	if err != nil {
		t.Fatalf("derive boundary policy-group map: %v", err)
	}
	actual := struct {
		Count  int    `json:"count"`
		SHA256 string `json:"policy_group_map_sha256"`
	}{Count: len(groups), SHA256: string(digest)}
	assertVectorValue(t, actual, expected)
}

func validateFixturePolicyGroupGeneratorShape(t testing.TB, raw json.RawMessage, path string) {
	t.Helper()
	fixtureRawObject(
		t, raw, path, "grammar", "count", "first_index", "index_width", "index_radix", "group_id_template",
		"rate_scope_id_template", "request_start_limit", "concurrency", "interval_ms",
	)
}

func fixturePolicyGroupsFromGenerator(t testing.TB, generator fixturePolicyGroupGenerator) []PolicyGroup {
	t.Helper()
	if generator.Grammar != "indexed_policy_groups_v1" || generator.Count < 0 || generator.Count > MaxPolicyGroupsPerRun+1 ||
		generator.FirstIndex < 0 || generator.IndexWidth < 1 || generator.IndexWidth > 16 ||
		(generator.IndexRadix != 10 && generator.IndexRadix != 16) ||
		strings.Count(generator.GroupIDTemplate, "{index}") != 1 ||
		strings.Count(generator.RateScopeIDTemplate, "{index}") != 1 {
		t.Fatal("invalid indexed policy-group generator grammar")
	}
	groups := make([]PolicyGroup, 0, generator.Count)
	for offset := 0; offset < generator.Count; offset++ {
		index := generator.FirstIndex + offset
		indexText := strconv.FormatInt(int64(index), generator.IndexRadix)
		if len(indexText) > generator.IndexWidth {
			t.Fatal("policy-group generator index exceeds declared width")
		}
		indexText = strings.Repeat("0", generator.IndexWidth-len(indexText)) + indexText
		groupID, err := ParseGroupID(strings.Replace(generator.GroupIDTemplate, "{index}", indexText, 1))
		if err != nil {
			t.Fatalf("generated group ID: %v", err)
		}
		rateScopeID, err := ParseRateScopeID(strings.Replace(generator.RateScopeIDTemplate, "{index}", indexText, 1))
		if err != nil {
			t.Fatalf("generated rate-scope ID: %v", err)
		}
		groupScopeID, err := DeriveGroupScopeID(rateScopeID)
		if err != nil {
			t.Fatal(err)
		}
		groups = append(groups, PolicyGroup{
			GroupID: groupID, RateScopeID: rateScopeID, GroupScopeID: groupScopeID,
			RequestStartLimit: generator.RequestStartLimit, Concurrency: generator.Concurrency,
			IntervalMS: generator.IntervalMilliseconds,
		})
	}
	return groups
}

func verifyContractDigestCase(t testing.TB, vector digestVectorCase) contractCaseResult {
	t.Helper()
	input := decodeVectorPart[struct {
		Document struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
		} `json:"document"`
		LuaSources []struct {
			SourceName string `json:"source_name"`
			SourceText string `json:"source_text"`
		} `json:"lua_sources"`
	}](t, vector.Input, vector.Name+".input")
	expected := decodeVectorPart[contractCaseResult](t, vector.Expected, vector.Name+".expected")

	var documentBytes []byte
	switch input.Document.Kind {
	case "path":
		if input.Document.Value != "docs/crawl-jobs-v2.md" {
			t.Fatalf("contract path %q is not the normative V2 document", input.Document.Value)
		}
		path := filepath.Join(fixtureRepositoryRoot(t), filepath.FromSlash(input.Document.Value))
		var err error
		documentBytes, err = os.ReadFile(path)
		if err != nil {
			t.Fatalf("read contract document: %v", err)
		}
	case "utf8":
		documentBytes = []byte(input.Document.Value)
	default:
		t.Fatalf("unsupported contract document kind %q", input.Document.Kind)
	}
	if !utf8.Valid(documentBytes) {
		t.Fatal("contract document is not valid UTF-8 text")
	}

	type namedSource struct {
		name string
		text []byte
	}
	sources := make([]namedSource, 0, len(input.LuaSources))
	seen := make(map[string]struct{}, len(input.LuaSources))
	validSourceName := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
	for _, source := range input.LuaSources {
		if !validSourceName.MatchString(source.SourceName) || !isPrintableSourceASCII(source.SourceName) {
			t.Fatalf("invalid Lua source name %q", source.SourceName)
		}
		if _, duplicate := seen[source.SourceName]; duplicate {
			t.Fatalf("duplicate Lua source name %q", source.SourceName)
		}
		seen[source.SourceName] = struct{}{}
		sources = append(sources, namedSource{name: source.SourceName, text: []byte(source.SourceText)})
	}
	sort.Slice(sources, func(left, right int) bool {
		return bytes.Compare([]byte(sources[left].name), []byte(sources[right].name)) < 0
	})
	documentSection, err := EncodeSection("document", []Record{{{Name: "document_bytes", Value: documentBytes}}})
	if err != nil {
		t.Fatal(err)
	}
	luaRecords := make([]Record, len(sources))
	order := make([]string, len(sources))
	for index, source := range sources {
		order[index] = source.name
		luaRecords[index] = Record{textField("source_name", source.name), {Name: "source_bytes", Value: source.text}}
	}
	luaSection, err := EncodeSection("lua", luaRecords)
	if err != nil {
		t.Fatal(err)
	}
	actual := contractCaseResult{
		LuaSourceOrder: order,
		ContractSHA256: string(digestEncoded("mifolyo:crawl-contract:v2", documentSection, luaSection)),
	}
	assertVectorValue(t, actual, expected)
	return actual
}

type fixtureGuardCoreSpec struct {
	RedisVersion           string `json:"redis_version"`
	RedisConfigSHA256      string `json:"redis_config_sha256"`
	MaximumShapeSHA256     string `json:"maximum_shape_sha256"`
	MemoryFixtureSHA256    string `json:"memory_fixture_sha256"`
	LuaBenchmarkSHA256     string `json:"lua_benchmark_sha256"`
	AOFCrashEvidenceSHA256 string `json:"aof_crash_evidence_sha256"`
	CutoverMode            string `json:"cutover_mode"`
	CandidateRunID         string `json:"candidate_run_id"`
}

type fixtureCompatibilitySpec struct {
	RedisConfigSHA256       string `json:"redis_config_sha256"`
	SpiderImage             string `json:"spider_image"`
	SeedImporterImage       string `json:"seed_importer_image"`
	CrawlAdminImage         string `json:"crawl_admin_image"`
	IndexerImage            string `json:"indexer_image"`
	ImageIndexerImage       string `json:"image_indexer_image"`
	BacklinksProcessorImage string `json:"backlinks_processor_image"`
	MonitoringImage         string `json:"monitoring_image"`
	RenderWorkerImage       string `json:"render_worker_image"`
}

type fixtureGuardChainInput struct {
	GuardMode     string                   `json:"guard_mode"`
	ContractCase  string                   `json:"contract_case"`
	GuardCore     fixtureGuardCoreSpec     `json:"guard_core"`
	Compatibility fixtureCompatibilitySpec `json:"compatibility"`
	ApprovedAtMS  uint64                   `json:"approved_at_ms"`
}

type fixtureGuardCoreCaseInput struct {
	GuardMode    string               `json:"guard_mode"`
	ContractCase string               `json:"contract_case"`
	GuardCore    fixtureGuardCoreSpec `json:"guard_core"`
}

type fixtureGuardExpected struct {
	GuardCoreSHA256             string `json:"guard_core_sha256"`
	CompatibilityManifestSHA256 string `json:"compatibility_manifest_sha256"`
	CompatibilityMarkerSHA256   string `json:"compatibility_marker_sha256"`
	StoredGuardSHA256           string `json:"stored_guard_sha256"`
}

func (h *fixtureConformanceHarness) verifyGuardChainCase(t *testing.T, vector digestVectorCase) {
	t.Helper()
	input := decodeVectorPart[fixtureGuardChainInput](t, vector.Input, vector.Name+".input")
	expected := decodeVectorPart[fixtureGuardExpected](t, vector.Expected, vector.Name+".expected")
	if input.GuardMode != "production" {
		t.Fatalf("guard chain mode = %q, want production", input.GuardMode)
	}
	contract, ok := h.contractCases[input.ContractCase]
	if !ok {
		t.Fatalf("guard references contract case %q before it was consumed", input.ContractCase)
	}
	contractDigest := mustFixtureDigest(t, contract.ContractSHA256)
	coreInput := fixtureGuardCoreValue(t, contractDigest, input.GuardCore)
	core, err := NewGuardCore(coreInput)
	if err != nil {
		t.Fatalf("construct guard core: %v", err)
	}
	coreDigest, err := core.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	coreBytes, err := core.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if fixtureSHA256(coreBytes) != string(coreDigest) {
		t.Fatal("guard core SHA-256 API differs from encoded RECORD digest")
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
	if err != nil {
		t.Fatalf("construct compatibility artifact: %v", err)
	}
	if err := ValidateGuardCompatibility(core, artifact); err != nil {
		t.Fatalf("bind guard and compatibility artifact: %v", err)
	}
	manifestDigest, err := artifact.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	artifactBytes, err := artifact.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if fixtureSHA256(artifactBytes) != string(manifestDigest) {
		t.Fatal("compatibility SHA-256 API differs from encoded RECORD digest")
	}
	marker, err := NewCompatibilityMarker(artifact)
	if err != nil {
		t.Fatal(err)
	}
	markerBytes, err := marker.Encode()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := NewStoredCommitGuard(core, manifestDigest, input.ApprovedAtMS)
	if err != nil {
		t.Fatal(err)
	}
	storedBytes, err := stored.Encode()
	if err != nil {
		t.Fatal(err)
	}
	actual := fixtureGuardExpected{
		GuardCoreSHA256:             string(coreDigest),
		CompatibilityManifestSHA256: string(manifestDigest),
		CompatibilityMarkerSHA256:   fixtureSHA256(markerBytes),
		StoredGuardSHA256:           fixtureSHA256(storedBytes),
	}
	assertVectorValue(t, actual, expected)
}

func (h *fixtureConformanceHarness) verifyGuardCoreCase(t *testing.T, vector digestVectorCase) {
	t.Helper()
	input := decodeVectorPart[fixtureGuardCoreCaseInput](t, vector.Input, vector.Name+".input")
	expected := decodeVectorPart[struct {
		GuardCoreSHA256 string `json:"guard_core_sha256"`
		RecordHex       string `json:"record_hex"`
	}](t, vector.Expected, vector.Name+".expected")
	contract, ok := h.contractCases[input.ContractCase]
	if !ok {
		t.Fatalf("guard core references contract case %q before it was consumed", input.ContractCase)
	}
	coreInput := fixtureGuardCoreValue(t, mustFixtureDigest(t, contract.ContractSHA256), input.GuardCore)
	var coreRecord Record
	var coreBytes []byte
	var coreDigest Digest
	var err error
	switch input.GuardMode {
	case "production":
		core, constructErr := NewGuardCore(coreInput)
		if constructErr != nil {
			t.Fatalf("construct production guard core: %v", constructErr)
		}
		coreRecord, err = core.Record()
		if err == nil {
			coreBytes, err = core.Encode()
		}
		if err == nil {
			coreDigest, err = core.SHA256()
		}
	case "provisional_fixture":
		core, constructErr := NewProvisionalGuardCore(coreInput)
		if constructErr != nil {
			t.Fatalf("construct provisional guard core: %v", constructErr)
		}
		coreRecord, err = core.Record()
		if err == nil {
			coreBytes, err = core.Encode()
		}
		if err == nil {
			coreDigest, err = core.SHA256()
		}
	default:
		t.Fatalf("unsupported guard mode %q", input.GuardMode)
	}
	if err != nil {
		t.Fatal(err)
	}
	wantNames := guardCoreFieldNames()
	if len(coreRecord) != len(wantNames) {
		t.Fatalf("guard core fields = %d, want %d", len(coreRecord), len(wantNames))
	}
	for index := range wantNames {
		if coreRecord[index].Name != wantNames[index] {
			t.Fatalf("guard core field %d = %q, want %q", index, coreRecord[index].Name, wantNames[index])
		}
	}
	ordinary, err := EncodeRecord(coreRecord)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ordinary, coreBytes) || fixtureSHA256(coreBytes) != string(coreDigest) {
		t.Fatal("guard core is not the exact ordinary RECORD encoding")
	}
	actual := struct {
		GuardCoreSHA256 string `json:"guard_core_sha256"`
		RecordHex       string `json:"record_hex"`
	}{GuardCoreSHA256: string(coreDigest), RecordHex: hex.EncodeToString(coreBytes)}
	assertVectorValue(t, actual, expected)
}

func fixtureGuardCoreValue(t testing.TB, contractDigest Digest, spec fixtureGuardCoreSpec) GuardCoreInput {
	t.Helper()
	return GuardCoreInput{
		ContractSHA256: contractDigest, RedisVersion: spec.RedisVersion,
		RedisConfigSHA256:      mustFixtureDigest(t, spec.RedisConfigSHA256),
		MaximumShapeSHA256:     mustFixtureDigest(t, spec.MaximumShapeSHA256),
		MemoryFixtureSHA256:    mustFixtureDigest(t, spec.MemoryFixtureSHA256),
		LuaBenchmarkSHA256:     mustFixtureDigest(t, spec.LuaBenchmarkSHA256),
		AOFCrashEvidenceSHA256: mustFixtureDigest(t, spec.AOFCrashEvidenceSHA256),
		CutoverMode:            CutoverMode(spec.CutoverMode), CandidateRunID: mustOptionalRunID(t, spec.CandidateRunID),
	}
}

func (h *fixtureConformanceHarness) verifyTransitionMutationCase(t *testing.T, vector digestVectorCase) {
	t.Helper()
	input := decodeVectorPart[struct {
		ChangedReason         string `json:"changed_reason"`
		ChangedPayloadOwnerID string `json:"changed_payload_owner_id"`
	}](t, vector.Input, vector.Name+".input")
	type transitionExpected struct {
		BasePayloadDigest          string `json:"base_payload_digest"`
		ChangedPayloadDigest       string `json:"changed_payload_digest"`
		BaseTransitionID           string `json:"base_transition_id"`
		ChangedReasonTransitionID  string `json:"changed_reason_transition_id"`
		ChangedPayloadTransitionID string `json:"changed_payload_transition_id"`
		ReplayRejectionClass       string `json:"replay_rejection_class"`
	}
	expected := decodeVectorPart[transitionExpected](t, vector.Expected, vector.Name+".expected")
	baseLease := vectorLease(t, h.fixture)
	changedLease := baseLease
	changedLease.OwnerID = mustOwnerID(t, input.ChangedPayloadOwnerID)
	basePayload, err := deriveTransitionPayloadDigest(Record{textField("owner_id", string(baseLease.OwnerID))})
	if err != nil {
		t.Fatal(err)
	}
	changedPayload, err := deriveTransitionPayloadDigest(Record{textField("owner_id", string(changedLease.OwnerID))})
	if err != nil {
		t.Fatal(err)
	}
	baseID, err := DeriveRetryTransitionID(RetryTransitionInput{Lease: baseLease, Reason: ReasonRequestTimeout})
	if err != nil {
		t.Fatal(err)
	}
	changedReason, err := ParseReason(input.ChangedReason)
	if err != nil {
		t.Fatal(err)
	}
	changedReasonID, err := DeriveRetryTransitionID(RetryTransitionInput{Lease: baseLease, Reason: changedReason})
	if err != nil {
		t.Fatal(err)
	}
	changedPayloadID, err := DeriveRetryTransitionID(RetryTransitionInput{Lease: changedLease, Reason: ReasonRequestTimeout})
	if err != nil {
		t.Fatal(err)
	}
	replayClass := fixtureReplayRejection(baseID, changedReasonID)
	if second := fixtureReplayRejection(baseID, changedPayloadID); second != replayClass {
		t.Fatalf("changed replay classes differ: reason=%q payload=%q", replayClass, second)
	}
	actual := transitionExpected{
		BasePayloadDigest:          string(basePayload),
		ChangedPayloadDigest:       string(changedPayload),
		BaseTransitionID:           string(baseID),
		ChangedReasonTransitionID:  string(changedReasonID),
		ChangedPayloadTransitionID: string(changedPayloadID),
		ReplayRejectionClass:       replayClass,
	}
	assertVectorValue(t, actual, expected)
}

func decodeVectorPart[T any](t testing.TB, raw json.RawMessage, path string) T {
	t.Helper()
	var value T
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return value
}

func assertVectorValue(t testing.TB, actual, expected any) {
	t.Helper()
	if reflect.DeepEqual(actual, expected) {
		return
	}
	actualJSON, _ := json.MarshalIndent(actual, "", "  ")
	expectedJSON, _ := json.MarshalIndent(expected, "", "  ")
	t.Fatalf("fixture value mismatch\ncomputed: %s\nexpected: %s", actualJSON, expectedJSON)
}

func fixtureSHA256(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func fixtureRepositoryRoot(t testing.TB) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate conformance source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../../../../.."))
}

func mustFixtureDigest(t testing.TB, value string) Digest {
	t.Helper()
	digest, err := ParseDigest(value)
	if err != nil {
		t.Fatalf("parse fixture digest %q: %v", value, err)
	}
	return digest
}

func mustOwnerID(t testing.TB, value string) OwnerID {
	t.Helper()
	owner, err := ParseOwnerID(value)
	if err != nil {
		t.Fatalf("parse fixture owner ID: %v", err)
	}
	return owner
}

func mustOptionalRunID(t testing.TB, value string) RunID {
	t.Helper()
	if value == "" {
		return ""
	}
	runID, err := ParseRunID(value)
	if err != nil {
		t.Fatalf("parse fixture candidate run ID: %v", err)
	}
	return runID
}

func mustImageDigest(t testing.TB, value string) ImageDigest {
	t.Helper()
	digest, err := ParseImageDigest(value)
	if err != nil {
		t.Fatalf("parse fixture image digest %q: %v", value, err)
	}
	return digest
}

func fixtureReplayRejection(stored, submitted Digest) string {
	if stored != submitted {
		return "IMMUTABLE_MISMATCH"
	}
	return ""
}

func isPrintableSourceASCII(value string) bool {
	for index := range value {
		if value[index] > 0x7f {
			return false
		}
	}
	return true
}
