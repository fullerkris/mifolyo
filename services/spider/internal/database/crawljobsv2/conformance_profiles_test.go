package crawljobsv2

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

var fixtureChunkKinds = [...]ChunkKind{
	ChunkPageFields,
	ChunkHTML,
	ChunkOriginalHTML,
	ChunkOutlinks,
	ChunkDiscoveries,
	ChunkAliases,
	ChunkImages,
	ChunkImageManifest,
}

type outputProfileExpected struct {
	OutputDigest  string              `json:"output_digest"`
	PublicationID string              `json:"publication_id"`
	CommitID      string              `json:"commit_id"`
	Counts        map[string]int      `json:"counts"`
	SectionSHA256 map[string]string   `json:"section_sha256"`
	ChunkDigests  map[string][]string `json:"chunk_digests"`
}

type fixtureOutputSections struct {
	Page        []Record
	Outlinks    []Record
	Images      []Record
	Discoveries []Record
	Aliases     []Record
}

type fixtureOutputProfile struct {
	context         OutputContext
	output          CrawlOutput
	source          SourceJob
	sections        fixtureOutputSections
	chunks          map[string][]StageChunk
	result          outputProfileExpected
	generatorSHA256 string
}

type sourceProfileResult struct {
	Count         int    `json:"count"`
	SectionSHA256 string `json:"section_sha256"`
	SourceSHA256  string `json:"source_sha256"`
}

type fixtureOutputProfileInput struct {
	Profile   string                         `json:"profile"`
	Generator *fixtureMaximumOutputGenerator `json:"generator,omitempty"`
}

type fixtureSourceProfileInput struct {
	Profile   string                         `json:"profile"`
	Generator *fixtureMaximumSourceGenerator `json:"generator,omitempty"`
}

func (h *fixtureConformanceHarness) verifyOutputProfileCase(t *testing.T, vector digestVectorCase) {
	t.Helper()
	validateFixtureOutputProfileInputShape(t, vector.Input, vector.Name+".input")
	input := decodeVectorPart[fixtureOutputProfileInput](t, vector.Input, vector.Name+".input")
	expected := decodeVectorPart[outputProfileExpected](t, vector.Expected, vector.Name+".expected")
	actual := h.outputProfileWithGenerator(t, input.Profile, input.Generator).result
	assertVectorValue(t, actual, expected)
}

func (h *fixtureConformanceHarness) verifySourceProfileCase(t *testing.T, vector digestVectorCase) {
	t.Helper()
	validateFixtureSourceProfileInputShape(t, vector.Input, vector.Name+".input")
	input := decodeVectorPart[fixtureSourceProfileInput](t, vector.Input, vector.Name+".input")
	expected := decodeVectorPart[sourceProfileResult](t, vector.Expected, vector.Name+".expected")
	actual := h.sourceProfile(t, input.Profile, input.Generator)
	assertVectorValue(t, actual, expected)
}

func (h *fixtureConformanceHarness) verifyStageChunksCase(t *testing.T, vector digestVectorCase) {
	t.Helper()
	validateFixtureOutputProfileInputShape(t, vector.Input, vector.Name+".input")
	input := decodeVectorPart[fixtureOutputProfileInput](t, vector.Input, vector.Name+".input")
	expected := decodeVectorPart[struct {
		ChunkDigests map[string][]string `json:"chunk_digests"`
	}](t, vector.Expected, vector.Name+".expected")
	actual := struct {
		ChunkDigests map[string][]string `json:"chunk_digests"`
	}{ChunkDigests: h.outputProfileWithGenerator(t, input.Profile, input.Generator).result.ChunkDigests}
	assertVectorValue(t, actual, expected)
}

func (h *fixtureConformanceHarness) outputProfile(t *testing.T, name string) *fixtureOutputProfile {
	return h.outputProfileWithGenerator(t, name, nil)
}

func (h *fixtureConformanceHarness) outputProfileWithGenerator(t *testing.T, name string, generator *fixtureMaximumOutputGenerator) *fixtureOutputProfile {
	t.Helper()
	if cached, ok := h.outputProfiles[name]; ok {
		if cached.generatorSHA256 != fixtureGeneratorSHA256(t, generator) {
			t.Fatalf("output profile %q was requested with conflicting declarative generators", name)
		}
		return cached
	}

	var context OutputContext
	var output CrawlOutput
	var source SourceJob
	var lease LeaseIdentity
	switch name {
	case "baseline":
		requireNoOutputGenerator(t, name, generator)
		context = vectorOutputContextValue(t, h.fixture)
		output = vectorOutputValue(t, h.fixture)
		source = vectorSourceJobValue(t, h.fixture, h.fixture.OutputContext.SourceJobIndex)
		lease = vectorLease(t, h.fixture)
	case "empty":
		requireNoOutputGenerator(t, name, generator)
		context = vectorOutputContextWith(t, h.fixture, 1, nil)
		output = cloneFixtureOutput(vectorOutputValue(t, h.fixture))
		output.Page.NormalizedURL = h.fixture.Targets["page"].CanonicalURL
		output.Page.HTML = []byte{}
		output.Page.OriginalHTML = []byte{}
		output.Page.ContentType = "text/html"
		output.Page.StatusCode = 100
		output.Page.Rendered = false
		output.Page.RenderPolicyRule = ""
		output.Page.RenderPolicyDigest = ""
		output.Outlinks = []string{}
		output.Images = []OutputImage{}
		output.Discoveries = []OutputDiscovery{}
		source = vectorSourceJobValue(t, h.fixture, h.fixture.OutputContext.SourceJobIndex)
		lease = vectorLease(t, h.fixture)
	case "rendered":
		requireNoOutputGenerator(t, name, generator)
		context = vectorOutputContextWith(t, h.fixture, len(h.fixture.OutputContext.Requests), []string{"render-main"})
		output = cloneFixtureOutput(vectorOutputValue(t, h.fixture))
		output.Page.HTML = []byte("<html>rendered</html>")
		output.Page.OriginalHTML = []byte("<html>source</html>")
		output.Page.Rendered = true
		output.Page.RenderPolicyRule = "render-main"
		output.Page.RenderPolicyDigest = context.renderPolicy.digest
		source = vectorSourceJobValue(t, h.fixture, h.fixture.OutputContext.SourceJobIndex)
		lease = vectorLease(t, h.fixture)
	case "maximum":
		if generator == nil {
			t.Fatal("maximum output profile is missing its declarative generator")
		}
		context, output, source, lease = h.buildMaximumOutput(t, *generator)
	default:
		t.Fatalf("unsupported output profile %q", name)
	}

	profile := buildFixtureOutputProfile(t, context, output, source, lease)
	profile.generatorSHA256 = fixtureGeneratorSHA256(t, generator)
	h.outputProfiles[name] = profile
	return profile
}

func buildFixtureOutputProfile(t *testing.T, context OutputContext, output CrawlOutput, source SourceJob, lease LeaseIdentity) *fixtureOutputProfile {
	t.Helper()
	pageRecord, err := outputPageRecord(context, output.Page)
	if err != nil {
		t.Fatalf("build output page record: %v", err)
	}
	outlinks, err := outputOutlinkRecords(output.Page.NormalizedURL, output.Outlinks)
	if err != nil {
		t.Fatalf("build output outlinks: %v", err)
	}
	images, err := outputImageRecords(output.Images)
	if err != nil {
		t.Fatalf("build output images: %v", err)
	}
	discoveries, err := outputDiscoveryRecords(output.Discoveries)
	if err != nil {
		t.Fatalf("build output discoveries: %v", err)
	}
	aliases, err := outputAliasRecords(context.aliases)
	if err != nil {
		t.Fatalf("build output aliases: %v", err)
	}
	sections := fixtureOutputSections{
		Page: []Record{pageRecord}, Outlinks: outlinks, Images: images,
		Discoveries: discoveries, Aliases: aliases,
	}
	encodedSections := make(map[string][]byte, 5)
	for label, records := range map[string][]Record{
		"page": sections.Page, "outlinks": sections.Outlinks, "images": sections.Images,
		"discoveries": sections.Discoveries, "aliases": sections.Aliases,
	} {
		encodedSections[label], err = EncodeSection(label, records)
		if err != nil {
			t.Fatalf("encode %s output section: %v", label, err)
		}
	}
	outputDigest, err := DeriveOutputDigest(context, output)
	if err != nil {
		t.Fatalf("derive output profile digest: %v", err)
	}
	assembledDigest := digestEncoded(
		"mifolyo:crawl-output:v2",
		encodedSections["page"], encodedSections["outlinks"], encodedSections["images"],
		encodedSections["discoveries"], encodedSections["aliases"],
	)
	if assembledDigest != outputDigest {
		t.Fatalf("assembled output digest %s differs from package API %s", assembledDigest, outputDigest)
	}
	if !context.initialized || context.jobID != source.JobID || lease.JobID != source.JobID || context.lease != lease {
		t.Fatal("profile context/source/lease mismatch")
	}
	publicationID, err := DerivePublicationID(PublicationIdentity{
		RunID: lease.RunID, JobID: lease.JobID, Fence: lease.Fence, OutputDigest: outputDigest,
	})
	if err != nil {
		t.Fatalf("derive output profile publication: %v", err)
	}
	commitID, err := DeriveCommitID(CommitIdentity{
		RunID: lease.RunID, JobID: lease.JobID, OwnerID: lease.OwnerID,
		Fence: lease.Fence, Token: lease.Token, PublicationID: publicationID,
		RequestStartsBaseline: context.requestStartsBaseline, RequestStartsGeneration: context.requestStartsGeneration,
	})
	if err != nil {
		t.Fatalf("derive output profile commit: %v", err)
	}
	chunks, chunkDigests := buildFixtureStageChunks(t, commitID, publicationID, context, output)
	sectionDigests := make(map[string]string, len(encodedSections))
	for label, encoded := range encodedSections {
		sectionDigests[label] = fixtureSHA256(encoded)
	}
	return &fixtureOutputProfile{
		context:  context,
		output:   output,
		source:   source,
		sections: sections,
		chunks:   chunks,
		result: outputProfileExpected{
			OutputDigest: string(outputDigest), PublicationID: string(publicationID), CommitID: string(commitID),
			Counts: map[string]int{
				"page": len(sections.Page), "outlinks": len(sections.Outlinks), "images": len(sections.Images),
				"discoveries": len(sections.Discoveries), "aliases": len(sections.Aliases),
			},
			SectionSHA256: sectionDigests,
			ChunkDigests:  chunkDigests,
		},
	}
}

func buildFixtureStageChunks(
	t *testing.T,
	commitID Digest,
	publicationID Digest,
	context OutputContext,
	output CrawlOutput,
) (map[string][]StageChunk, map[string][]string) {
	t.Helper()
	identity := outputCommitIdentity(context, publicationID)
	if expected, err := DeriveCommitID(identity); err != nil || expected != commitID {
		t.Fatal("fixture stage commit/context mismatch")
	}
	if err := ValidateOutputCommit(context, identity, output); err != nil {
		t.Fatalf("fixture pre-seal output/context mismatch: %v", err)
	}
	chunks := make(map[string][]StageChunk, len(fixtureChunkKinds))
	digests := make(map[string][]string, len(fixtureChunkKinds))
	for _, kind := range fixtureChunkKinds {
		chunks[string(kind)] = make([]StageChunk, 0)
		digests[string(kind)] = make([]string, 0)
	}
	add := func(chunk StageChunk, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("construct stage chunk: %v", err)
		}
		digest, err := DeriveChunkDigest(chunk)
		if err != nil {
			t.Fatalf("derive %s stage chunk: %v", chunk.Kind(), err)
		}
		kind := string(chunk.Kind())
		chunks[kind] = append(chunks[kind], chunk)
		digests[kind] = append(digests[kind], string(digest))
	}
	add(NewPageFieldsStageChunk(identity, context, output.Page))
	add(NewPageBlobStageChunk(identity, context, ChunkHTML, output.Page.HTML))
	add(NewPageBlobStageChunk(identity, context, ChunkOriginalHTML, output.Page.OriginalHTML))

	orderedOutlinks := append([]string(nil), output.Outlinks...)
	sort.Strings(orderedOutlinks)
	for first := 0; first < len(orderedOutlinks); first += MaxNonBlobStageBatchRecords {
		last := first + MaxNonBlobStageBatchRecords
		if last > len(orderedOutlinks) {
			last = len(orderedOutlinks)
		}
		add(NewOutlinksStageChunk(identity, uint64(first/MaxNonBlobStageBatchRecords), context, orderedOutlinks[first:last]))
	}

	orderedDiscoveries := append([]OutputDiscovery(nil), output.Discoveries...)
	sort.Slice(orderedDiscoveries, func(left, right int) bool {
		return string(orderedDiscoveries[left].JobID) < string(orderedDiscoveries[right].JobID)
	})
	for first := 0; first < len(orderedDiscoveries); first += MaxNonBlobStageBatchRecords {
		last := first + MaxNonBlobStageBatchRecords
		if last > len(orderedDiscoveries) {
			last = len(orderedDiscoveries)
		}
		add(NewDiscoveriesStageChunk(identity, uint64(first/MaxNonBlobStageBatchRecords), context, orderedDiscoveries[first:last]))
	}
	add(NewAliasesStageChunk(identity, context))
	if len(output.Images) != 0 {
		add(NewImagesStageChunk(identity, context, output.Images))
	}
	add(NewImageManifestStageChunk(identity, context, output.Images))
	return chunks, digests
}

type fixtureUTF8RepeatGenerator struct {
	Text              string `json:"text"`
	RepeatCount       int    `json:"repeat_count"`
	ExpectedUTF8Bytes int    `json:"expected_utf8_bytes"`
}

type fixtureIndexedTemplateGenerator struct {
	Grammar     string `json:"grammar"`
	Template    string `json:"template"`
	Count       int    `json:"count"`
	FirstIndex  int    `json:"first_index"`
	IndexWidth  int    `json:"index_width"`
	IndexRadix  int    `json:"index_radix"`
	FillToBytes int    `json:"fill_to_bytes"`
	FillByteHex string `json:"fill_byte_hex"`
	InputOrder  string `json:"input_order"`
}

type fixturePaddedTextGenerator struct {
	Prefix         string `json:"prefix"`
	FillText       string `json:"fill_text"`
	Suffix         string `json:"suffix"`
	TotalUTF8Bytes int    `json:"total_utf8_bytes"`
}

type fixtureGeneratedPolicyGroup struct {
	GroupID           fixtureUTF8RepeatGenerator `json:"group_id"`
	RateScopeID       fixtureUTF8RepeatGenerator `json:"rate_scope_id"`
	RequestStartLimit uint64                     `json:"request_start_limit"`
	GlobalConcurrency uint64                     `json:"global_concurrency"`
	GlobalIntervalMS  uint64                     `json:"global_interval_ms"`
	Concurrency       uint64                     `json:"concurrency"`
	IntervalMS        uint64                     `json:"interval_ms"`
	OriginConcurrency uint64                     `json:"origin_concurrency"`
	OriginIntervalMS  uint64                     `json:"origin_interval_ms"`
}

type fixtureMaximumIdentityGenerator struct {
	RunID             string `json:"run_id"`
	JobIDFrom         string `json:"job_id_from"`
	OwnerID           string `json:"owner_id"`
	AlternateOwnerID  string `json:"alternate_owner_id"`
	LeaseToken        string `json:"lease_token"`
	Fence             uint64 `json:"fence"`
	RateScopeIDFrom   string `json:"rate_scope_id_from"`
	CrawlPolicySHA256 string `json:"crawl_policy_sha256"`
}

type fixtureGeneratedSource struct {
	TargetIndex    int    `json:"target_index"`
	ScoreText      string `json:"score_text"`
	Depth          uint64 `json:"depth"`
	RequestKind    string `json:"request_kind"`
	PolicyGroupRef string `json:"policy_group_ref"`
}

type fixtureGeneratedRequestChain struct {
	InitialRequestKind              string   `json:"initial_request_kind"`
	SubsequentRequestKind           string   `json:"subsequent_request_kind"`
	FirstStartedAtMS                uint64   `json:"first_started_at_ms"`
	StartedAtStepMS                 uint64   `json:"started_at_step_ms"`
	PolicyGroupRef                  string   `json:"policy_group_ref"`
	LeaseRequestStartsBaseline      string   `json:"lease_request_starts_baseline"`
	TerminalRequestStartsGeneration string   `json:"terminal_request_starts_generation"`
	JobRequestStarts                []string `json:"job_request_starts"`
	RequestOrdinals                 []string `json:"request_ordinals"`
}

type fixtureGeneratedPage struct {
	NormalizedTargetIndex int                        `json:"normalized_target_index"`
	HTML                  fixtureUTF8RepeatGenerator `json:"html"`
	OriginalHTML          fixtureUTF8RepeatGenerator `json:"original_html"`
	ContentType           fixturePaddedTextGenerator `json:"content_type"`
	StatusCode            uint16                     `json:"status_code"`
	Rendered              bool                       `json:"rendered"`
	RenderPolicyRule      fixtureUTF8RepeatGenerator `json:"render_policy_rule"`
	RenderPolicySHA256    string                     `json:"render_policy_sha256"`
}

type fixtureGeneratedImages struct {
	URLs fixtureIndexedTemplateGenerator `json:"urls"`
	Alt  fixtureUTF8RepeatGenerator      `json:"alt"`
}

type fixtureGeneratedDiscoveries struct {
	URLs           fixtureIndexedTemplateGenerator `json:"urls"`
	ScoreText      string                          `json:"score_text"`
	Depth          uint64                          `json:"depth"`
	RequestKind    string                          `json:"request_kind"`
	PolicyGroupRef string                          `json:"policy_group_ref"`
}

type fixtureMaximumOutputGenerator struct {
	Grammar      string                          `json:"grammar"`
	Identity     fixtureMaximumIdentityGenerator `json:"identity"`
	PolicyGroup  fixtureGeneratedPolicyGroup     `json:"policy_group"`
	Aliases      fixtureIndexedTemplateGenerator `json:"aliases"`
	Source       fixtureGeneratedSource          `json:"source"`
	RequestChain fixtureGeneratedRequestChain    `json:"request_chain"`
	Page         fixtureGeneratedPage            `json:"page"`
	Outlinks     fixtureIndexedTemplateGenerator `json:"outlinks"`
	Images       fixtureGeneratedImages          `json:"images"`
	Discoveries  fixtureGeneratedDiscoveries     `json:"discoveries"`
}

type fixtureMaximumSourceGenerator struct {
	Grammar     string                          `json:"grammar"`
	PolicyGroup fixtureGeneratedPolicyGroup     `json:"policy_group"`
	Jobs        fixtureIndexedTemplateGenerator `json:"jobs"`
	Source      struct {
		ScoreText      string `json:"score_text"`
		Depth          uint64 `json:"depth"`
		RequestKind    string `json:"request_kind"`
		PolicyGroupRef string `json:"policy_group_ref"`
	} `json:"source"`
}

func validateFixtureOutputProfileInputShape(t testing.TB, raw json.RawMessage, path string) {
	t.Helper()
	input := decodeVectorPart[map[string]json.RawMessage](t, raw, path)
	profileRaw, ok := input["profile"]
	if !ok {
		t.Fatalf("%s is missing key %q", path, "profile")
	}
	profile := decodeVectorPart[string](t, profileRaw, path+".profile")
	if profile != "maximum" {
		fixtureRequireObjectKeys(t, input, path, "profile")
		return
	}
	fixtureRequireObjectKeys(t, input, path, "profile", "generator")
	validateFixtureMaximumOutputGeneratorShape(t, input["generator"], path+".generator")
}

func validateFixtureSourceProfileInputShape(t testing.TB, raw json.RawMessage, path string) {
	t.Helper()
	input := decodeVectorPart[map[string]json.RawMessage](t, raw, path)
	profileRaw, ok := input["profile"]
	if !ok {
		t.Fatalf("%s is missing key %q", path, "profile")
	}
	profile := decodeVectorPart[string](t, profileRaw, path+".profile")
	if profile != "maximum" {
		fixtureRequireObjectKeys(t, input, path, "profile")
		return
	}
	fixtureRequireObjectKeys(t, input, path, "profile", "generator")
	generator := fixtureRawObject(t, input["generator"], path+".generator", "grammar", "policy_group", "jobs", "source")
	validateFixtureGeneratedPolicyGroupShape(t, generator["policy_group"], path+".generator.policy_group")
	validateFixtureIndexedTemplateShape(t, generator["jobs"], path+".generator.jobs")
	fixtureRawObject(t, generator["source"], path+".generator.source", "score_text", "depth", "request_kind", "policy_group_ref")
}

func validateFixtureMaximumOutputGeneratorShape(t testing.TB, raw json.RawMessage, path string) {
	t.Helper()
	generator := fixtureRawObject(
		t, raw, path, "grammar", "identity", "policy_group", "aliases", "source", "request_chain",
		"page", "outlinks", "images", "discoveries",
	)
	fixtureRawObject(
		t, generator["identity"], path+".identity", "run_id", "job_id_from", "owner_id", "alternate_owner_id",
		"lease_token", "fence", "rate_scope_id_from", "crawl_policy_sha256",
	)
	validateFixtureGeneratedPolicyGroupShape(t, generator["policy_group"], path+".policy_group")
	validateFixtureIndexedTemplateShape(t, generator["aliases"], path+".aliases")
	fixtureRawObject(t, generator["source"], path+".source", "target_index", "score_text", "depth", "request_kind", "policy_group_ref")
	fixtureRawObject(
		t, generator["request_chain"], path+".request_chain", "initial_request_kind", "subsequent_request_kind",
		"first_started_at_ms", "started_at_step_ms", "policy_group_ref", "lease_request_starts_baseline",
		"terminal_request_starts_generation", "job_request_starts", "request_ordinals",
	)
	page := fixtureRawObject(
		t, generator["page"], path+".page", "normalized_target_index", "html", "original_html", "content_type",
		"status_code", "rendered", "render_policy_rule", "render_policy_sha256",
	)
	validateFixtureRepeatShape(t, page["html"], path+".page.html")
	validateFixtureRepeatShape(t, page["original_html"], path+".page.original_html")
	fixtureRawObject(t, page["content_type"], path+".page.content_type", "prefix", "fill_text", "suffix", "total_utf8_bytes")
	validateFixtureRepeatShape(t, page["render_policy_rule"], path+".page.render_policy_rule")
	validateFixtureIndexedTemplateShape(t, generator["outlinks"], path+".outlinks")
	images := fixtureRawObject(t, generator["images"], path+".images", "urls", "alt")
	validateFixtureIndexedTemplateShape(t, images["urls"], path+".images.urls")
	validateFixtureRepeatShape(t, images["alt"], path+".images.alt")
	discoveries := fixtureRawObject(t, generator["discoveries"], path+".discoveries", "urls", "score_text", "depth", "request_kind", "policy_group_ref")
	validateFixtureIndexedTemplateShape(t, discoveries["urls"], path+".discoveries.urls")
}

func validateFixtureGeneratedPolicyGroupShape(t testing.TB, raw json.RawMessage, path string) {
	t.Helper()
	group := fixtureRawObject(
		t, raw, path, "group_id", "rate_scope_id", "request_start_limit", "global_concurrency", "global_interval_ms",
		"concurrency", "interval_ms", "origin_concurrency", "origin_interval_ms",
	)
	validateFixtureRepeatShape(t, group["group_id"], path+".group_id")
	validateFixtureRepeatShape(t, group["rate_scope_id"], path+".rate_scope_id")
}

func validateFixtureRepeatShape(t testing.TB, raw json.RawMessage, path string) {
	t.Helper()
	fixtureRawObject(t, raw, path, "text", "repeat_count", "expected_utf8_bytes")
}

func validateFixtureIndexedTemplateShape(t testing.TB, raw json.RawMessage, path string) {
	t.Helper()
	fixtureRawObject(
		t, raw, path, "grammar", "template", "count", "first_index", "index_width", "index_radix",
		"fill_to_bytes", "fill_byte_hex", "input_order",
	)
}

func fixtureRawObject(t testing.TB, raw json.RawMessage, path string, keys ...string) map[string]json.RawMessage {
	t.Helper()
	value := decodeVectorPart[map[string]json.RawMessage](t, raw, path)
	fixtureRequireObjectKeys(t, value, path, keys...)
	return value
}

func fixtureRequireObjectKeys(t testing.TB, value map[string]json.RawMessage, path string, keys ...string) {
	t.Helper()
	want := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		want[key] = struct{}{}
	}
	if len(value) != len(want) {
		t.Fatalf("%s key count = %d, want %d", path, len(value), len(want))
	}
	for key := range value {
		if _, ok := want[key]; !ok {
			t.Fatalf("%s contains unknown key %q", path, key)
		}
	}
	for key := range want {
		if _, ok := value[key]; !ok {
			t.Fatalf("%s is missing key %q", path, key)
		}
	}
}

func (h *fixtureConformanceHarness) buildMaximumOutput(t *testing.T, generator fixtureMaximumOutputGenerator) (OutputContext, CrawlOutput, SourceJob, LeaseIdentity) {
	t.Helper()
	if generator.Grammar != "maximum_output_v1" {
		t.Fatalf("unsupported maximum-output grammar %q", generator.Grammar)
	}
	group := fixtureGeneratedPolicyGroupValue(t, generator.PolicyGroup)
	groups := []PolicyGroup{group}
	aliasURLs := fixtureGeneratedURLs(t, generator.Aliases)
	if len(aliasURLs) != MaxAliasesPerJob {
		t.Fatalf("maximum alias generator count = %d, want %d", len(aliasURLs), MaxAliasesPerJob)
	}
	aliasTargets := make([]RequestTarget, len(aliasURLs))
	for index, canonicalURL := range aliasURLs {
		aliasTargets[index] = mustFixtureTargetForURL(t, canonicalURL)
	}
	if generator.Source.PolicyGroupRef != "policy_group" || generator.Source.TargetIndex < 0 || generator.Source.TargetIndex >= len(aliasTargets) {
		t.Fatal("invalid generated source reference")
	}
	sourceKind, err := ParseRequestKind(generator.Source.RequestKind)
	if err != nil || sourceKind != RequestDocument {
		t.Fatalf("maximum source request kind: %v", err)
	}
	sourceTarget := aliasTargets[generator.Source.TargetIndex]
	sourceDecision := fixtureGeneratedDecision(t, sourceKind, sourceTarget, generator.Source.Depth, group, generator.PolicyGroup)
	score, err := ParseScoreText(generator.Source.ScoreText)
	if err != nil {
		t.Fatal(err)
	}
	source := SourceJob{
		JobID: sourceTarget.URLID, CanonicalURL: sourceTarget.CanonicalURL, ScoreText: score,
		Depth: generator.Source.Depth, GroupID: group.GroupID, RateScopeID: group.RateScopeID, Decision: sourceDecision,
	}
	if generator.Identity.RateScopeIDFrom != "policy_group.rate_scope_id" {
		t.Fatalf("unsupported generated rate-scope source %q", generator.Identity.RateScopeIDFrom)
	}
	lease := fixtureGeneratedLease(t, generator.Identity, source.JobID)
	policyDigest := mustFixtureDigest(t, generator.Identity.CrawlPolicySHA256)
	if generator.Page.RenderPolicySHA256 != generator.Identity.CrawlPolicySHA256 {
		t.Fatal("maximum page render digest is not the declared identity policy digest")
	}
	if generator.RequestChain.PolicyGroupRef != "policy_group" || generator.RequestChain.StartedAtStepMS == 0 {
		t.Fatal("invalid generated request-chain policy reference or timestamp step")
	}
	initialKind, err := ParseRequestKind(generator.RequestChain.InitialRequestKind)
	if err != nil {
		t.Fatal(err)
	}
	subsequentKind, err := ParseRequestKind(generator.RequestChain.SubsequentRequestKind)
	if err != nil {
		t.Fatal(err)
	}
	startedAt := make([]uint64, len(aliasTargets))
	for index := range startedAt {
		startedAt[index] = generator.RequestChain.FirstStartedAtMS + uint64(index)*generator.RequestChain.StartedAtStepMS
	}
	renderRule := fixtureRepeatedText(t, generator.Page.RenderPolicyRule)
	context := buildOpaqueFixtureOutputContext(
		t, source, lease, aliasTargets, startedAt, policyDigest, []string{renderRule},
		initialKind, subsequentKind, groups, generator.RequestChain,
	)

	outlinks := fixtureGeneratedURLs(t, generator.Outlinks)
	if len(outlinks) != MaxOutlinksPerJob {
		t.Fatalf("maximum outlink generator count = %d, want %d", len(outlinks), MaxOutlinksPerJob)
	}
	imageURLs := fixtureGeneratedURLs(t, generator.Images.URLs)
	if len(imageURLs) != MaxImagesPerPage {
		t.Fatalf("maximum image generator count = %d, want %d", len(imageURLs), MaxImagesPerPage)
	}
	alt := fixtureRepeatedText(t, generator.Images.Alt)
	images := make([]OutputImage, len(imageURLs))
	for index, canonicalURL := range imageURLs {
		images[index] = OutputImage{NormalizedSourceURL: canonicalURL, Alt: alt}
	}
	if generator.Discoveries.PolicyGroupRef != "policy_group" {
		t.Fatal("invalid discovery policy-group reference")
	}
	discoveryKind, err := ParseRequestKind(generator.Discoveries.RequestKind)
	if err != nil || discoveryKind != RequestDocument {
		t.Fatalf("maximum discovery request kind: %v", err)
	}
	discoveryScore, err := ParseScoreText(generator.Discoveries.ScoreText)
	if err != nil {
		t.Fatal(err)
	}
	discoveryURLs := fixtureGeneratedURLs(t, generator.Discoveries.URLs)
	if len(discoveryURLs) != MaxDiscoveriesPerJob {
		t.Fatalf("maximum discovery generator count = %d, want %d", len(discoveryURLs), MaxDiscoveriesPerJob)
	}
	discoveries := make([]OutputDiscovery, len(discoveryURLs))
	for index, canonicalURL := range discoveryURLs {
		target := mustFixtureTargetForURL(t, canonicalURL)
		decision := fixtureGeneratedDecision(t, discoveryKind, target, generator.Discoveries.Depth, group, generator.PolicyGroup)
		discoveries[index] = OutputDiscovery{
			JobID: target.URLID, CanonicalURL: target.CanonicalURL, Depth: generator.Discoveries.Depth,
			ScoreText: discoveryScore, GroupID: group.GroupID, RateScopeID: group.RateScopeID, Decision: decision,
		}
	}
	if generator.Page.NormalizedTargetIndex < 0 || generator.Page.NormalizedTargetIndex >= len(aliasTargets) {
		t.Fatal("maximum normalized target index is out of range")
	}
	output := CrawlOutput{
		Page: OutputPage{
			NormalizedURL: aliasTargets[generator.Page.NormalizedTargetIndex].CanonicalURL,
			HTML:          []byte(fixtureRepeatedText(t, generator.Page.HTML)),
			OriginalHTML:  []byte(fixtureRepeatedText(t, generator.Page.OriginalHTML)),
			ContentType:   fixturePaddedText(t, generator.Page.ContentType), StatusCode: generator.Page.StatusCode,
			Rendered: generator.Page.Rendered, RenderPolicyRule: renderRule, RenderPolicyDigest: context.renderPolicy.digest,
		},
		Outlinks: outlinks, Images: images, Discoveries: discoveries,
	}
	if err := ValidateRunPinnedPolicyBindings(context.runPolicy, RunPinnedPolicyBindings{
		Decisions: []PolicyDecision{sourceDecision}, SourceJobs: []SourceJob{source}, Discoveries: discoveries,
	}); err != nil {
		t.Fatalf("maximum output policy binding: %v", err)
	}
	return context, output, source, lease
}

func fixtureGeneratedPolicyGroupValue(t testing.TB, spec fixtureGeneratedPolicyGroup) PolicyGroup {
	t.Helper()
	groupID, err := ParseGroupID(fixtureRepeatedText(t, spec.GroupID))
	if err != nil {
		t.Fatal(err)
	}
	rateScopeID, err := ParseRateScopeID(fixtureRepeatedText(t, spec.RateScopeID))
	if err != nil {
		t.Fatal(err)
	}
	groupScopeID, err := DeriveGroupScopeID(rateScopeID)
	if err != nil {
		t.Fatal(err)
	}
	if spec.GlobalConcurrency != GlobalActiveRequestLimit || spec.GlobalIntervalMS != GlobalScopeIntervalMilliseconds ||
		spec.OriginConcurrency != spec.Concurrency || spec.OriginIntervalMS != spec.IntervalMS {
		t.Fatal("generated policy tuples disagree")
	}
	group := PolicyGroup{
		GroupID: groupID, RateScopeID: rateScopeID, GroupScopeID: groupScopeID,
		RequestStartLimit: spec.RequestStartLimit, Concurrency: spec.Concurrency, IntervalMS: spec.IntervalMS,
	}
	if _, err := policyGroupRecord(group); err != nil {
		t.Fatalf("generated policy group: %v", err)
	}
	return group
}

func fixtureGeneratedDecision(t testing.TB, kind RequestKind, target RequestTarget, depth uint64, group PolicyGroup, spec fixtureGeneratedPolicyGroup) PolicyDecision {
	t.Helper()
	decision, err := NewPolicyDecision(PolicyDecisionInput{
		RequestKind: kind, Target: target, Depth: depth, GroupID: group.GroupID, RateScopeID: group.RateScopeID,
		GroupConcurrency: spec.Concurrency, GroupIntervalMS: spec.IntervalMS,
		OriginConcurrency: spec.OriginConcurrency, OriginIntervalMS: spec.OriginIntervalMS,
	})
	if err != nil {
		t.Fatalf("construct generated policy decision: %v", err)
	}
	if err := ValidatePreRunPolicyDecisionGroupBinding(decision, []PolicyGroup{group}); err != nil {
		t.Fatalf("standalone generated policy decision/group check: %v", err)
	}
	return decision
}

func fixtureGeneratedLease(t testing.TB, spec fixtureMaximumIdentityGenerator, jobID JobID) LeaseIdentity {
	t.Helper()
	if spec.JobIDFrom != "source_target_url_id" {
		t.Fatalf("unsupported generated job-ID source %q", spec.JobIDFrom)
	}
	runID, err := ParseRunID(spec.RunID)
	if err != nil {
		t.Fatal(err)
	}
	ownerID, err := ParseOwnerID(spec.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseOwnerID(spec.AlternateOwnerID); err != nil {
		t.Fatal(err)
	}
	token, err := ParseLeaseToken(spec.LeaseToken)
	if err != nil {
		t.Fatal(err)
	}
	fence, err := NewFence(spec.Fence)
	if err != nil {
		t.Fatal(err)
	}
	return LeaseIdentity{RunID: runID, JobID: jobID, OwnerID: ownerID, Fence: fence, Token: token}
}

func fixtureRepeatedText(t testing.TB, generator fixtureUTF8RepeatGenerator) string {
	t.Helper()
	if generator.Text == "" || generator.RepeatCount < 1 || generator.ExpectedUTF8Bytes < 1 {
		t.Fatal("invalid repeated-text generator")
	}
	value := strings.Repeat(generator.Text, generator.RepeatCount)
	if len(value) != generator.ExpectedUTF8Bytes {
		t.Fatalf("repeated-text bytes = %d, declared %d", len(value), generator.ExpectedUTF8Bytes)
	}
	return value
}

func fixturePaddedText(t testing.TB, generator fixturePaddedTextGenerator) string {
	t.Helper()
	if len(generator.FillText) == 0 || generator.TotalUTF8Bytes < len(generator.Prefix)+len(generator.Suffix) {
		t.Fatal("invalid padded-text generator")
	}
	remaining := generator.TotalUTF8Bytes - len(generator.Prefix) - len(generator.Suffix)
	if remaining%len(generator.FillText) != 0 {
		t.Fatal("padded-text width is not divisible by fill width")
	}
	value := generator.Prefix + strings.Repeat(generator.FillText, remaining/len(generator.FillText)) + generator.Suffix
	if len(value) != generator.TotalUTF8Bytes {
		t.Fatal("padded-text generator did not produce its declared width")
	}
	return value
}

func fixtureGeneratedURLs(t testing.TB, generator fixtureIndexedTemplateGenerator) []string {
	t.Helper()
	if generator.Grammar != "indexed_template_v1" || generator.Count < 1 || generator.Count > MaxJobsPerRun ||
		generator.FirstIndex < 0 || generator.IndexWidth < 1 || generator.IndexWidth > 16 ||
		(generator.IndexRadix != 10 && generator.IndexRadix != 16) || strings.Count(generator.Template, "{index}") != 1 ||
		(generator.InputOrder != "ascending" && generator.InputOrder != "descending") {
		t.Fatal("invalid indexed-template generator")
	}
	fill, err := hex.DecodeString(generator.FillByteHex)
	if err != nil || len(fill) != 1 {
		t.Fatal("indexed-template fill_byte_hex must encode exactly one byte")
	}
	values := make([]string, generator.Count)
	for offset := 0; offset < generator.Count; offset++ {
		indexText := strconv.FormatInt(int64(generator.FirstIndex+offset), generator.IndexRadix)
		if len(indexText) > generator.IndexWidth {
			t.Fatal("indexed-template value exceeds declared width")
		}
		indexText = strings.Repeat("0", generator.IndexWidth-len(indexText)) + indexText
		value := strings.Replace(generator.Template, "{index}", indexText, 1)
		if generator.FillToBytes != 0 {
			if len(value) > generator.FillToBytes {
				t.Fatal("indexed-template prefix exceeds fill_to_bytes")
			}
			value += strings.Repeat(string(fill), generator.FillToBytes-len(value))
		}
		if _, err := requireCanonicalURL(value); err != nil {
			t.Fatalf("generated indexed URL: %v", err)
		}
		values[offset] = value
	}
	if generator.InputOrder == "descending" {
		reverseStrings(values)
	}
	return values
}

func fixtureGeneratorSHA256(t testing.TB, generator any) string {
	t.Helper()
	encoded, err := json.Marshal(generator)
	if err != nil {
		t.Fatalf("encode declarative generator: %v", err)
	}
	return fixtureSHA256(encoded)
}

func requireNoOutputGenerator(t testing.TB, profile string, generator *fixtureMaximumOutputGenerator) {
	t.Helper()
	if generator != nil {
		t.Fatalf("non-maximum output profile %q must not provide a generator", profile)
	}
}

func buildOpaqueFixtureOutputContext(
	t *testing.T,
	source SourceJob,
	lease LeaseIdentity,
	targets []RequestTarget,
	startedAt []uint64,
	policyDigest Digest,
	enabledRenderRules []string,
	initialKind RequestKind,
	subsequentKind RequestKind,
	policyGroups []PolicyGroup,
	requestChain fixtureGeneratedRequestChain,
) OutputContext {
	t.Helper()
	if len(targets) == 0 || len(targets) != len(startedAt) || len(targets) != len(requestChain.JobRequestStarts) || len(targets) != len(requestChain.RequestOrdinals) {
		t.Fatal("invalid generated request chain")
	}
	finalTarget := targets[len(targets)-1]
	var renderPolicyArtifact []byte
	switch len(enabledRenderRules) {
	case 0:
		renderPolicyArtifact = testDenyAllRenderPolicyArtifact()
	case 1:
		renderPolicyArtifact = testRenderPolicyArtifactForTarget(t, enabledRenderRules[0], true, finalTarget)
	default:
		t.Fatal("generated output context supports at most one enabled render rule")
	}
	renderPolicySHA256 := plainSHA256(renderPolicyArtifact)
	if len(enabledRenderRules) != 0 {
		// The historical maximum-shape vector pins a synthetic all-'a'
		// render digest and has no corresponding artifact preimage.
		renderPolicySHA256 = policyDigest
	}
	runPolicy := newAuthenticatedTestRunPolicyAuthority(
		t, lease.RunID, policyDigest, renderPolicySHA256, policyGroups,
	)
	var transcript DocumentTranscript
	transport := newTestTransportAuthority()
	baseline, err := parseResponseUint(requestChain.LeaseRequestStartsBaseline)
	if err != nil {
		t.Fatal(err)
	}
	attempts := uint64(1)
	if baseline > 0 {
		attempts = 2
	}
	for index, target := range targets {
		kind := subsequentKind
		if index == 0 {
			kind = initialKind
		}
		decision := mustFixtureDecision(
			t, kind, target, source.Depth, source.GroupID, source.RateScopeID,
			source.Decision.GroupConcurrency, source.Decision.GroupIntervalMS,
		)
		if err := ValidatePreRunPolicyDecisionGroupBinding(decision, policyGroups); err != nil {
			t.Fatalf("bind generated request policy decision: %v", err)
		}
		ordinal, err := parseResponseUint(requestChain.RequestOrdinals[index])
		if err != nil {
			t.Fatalf("parse generated request ordinal: %v", err)
		}
		intent := ReservationIntent{
			Lease: lease, RequestOrdinal: ordinal, Target: target,
			CrawlPolicyDigest: policyDigest, Decision: decision,
		}
		reservationID, err := DeriveReservationID(runPolicy, intent)
		if err != nil {
			t.Fatalf("derive generated reservation: %v", err)
		}
		starts := requestChain.JobRequestStarts[index]
		response, err := transport.parseStartRequestResponse(runPolicy, intent, []string{
			string(StatusStarted), canonicalDecimal(startedAt[index]), string(reservationID),
			canonicalDecimal(startedAt[index]), canonicalDecimal(attempts), starts, starts, starts, "1",
		})
		if err != nil {
			t.Fatalf("parse generated start response: %v", err)
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
			transcript, err = NewDocumentTranscript(runPolicy, source, evidence)
		} else {
			transcript, err = transcript.AppendRedirect(evidence)
		}
		if err != nil {
			t.Fatalf("append generated request evidence: %v", err)
		}
	}
	finalDigest, err := DeriveTargetDigest(finalTarget)
	if err != nil {
		t.Fatal(err)
	}
	witness, err := newTestTransportAuthority().parseFinalDocumentWitness(lease, []string{
		canonicalDecimal(startedAt[len(startedAt)-1]), canonicalDecimal(uint64(lease.Fence)),
		string(finalTarget.URLID), finalTarget.CanonicalURL, string(finalDigest),
		requestChain.TerminalRequestStartsGeneration, canonicalDecimal(startedAt[len(startedAt)-1]),
		requestChain.LeaseRequestStartsBaseline, "leased", string(lease.OwnerID), string(lease.Token), canonicalDecimal(uint64(lease.Fence)), "",
	})
	if err != nil {
		t.Fatalf("parse generated final witness: %v", err)
	}
	var authorization RenderPolicyAuthorization
	if len(enabledRenderRules) == 0 {
		authorization, err = NewRenderPolicyAuthorization(runPolicy, renderPolicyArtifact)
	} else {
		authorization = newNonAuthoritativeDigestVectorRenderProjection(t, runPolicy, renderPolicyArtifact)
	}
	if err != nil {
		t.Fatalf("authorize generated render policy: %v", err)
	}
	context, err := NewOutputContext(runPolicy, source, transcript, witness, authorization)
	if err != nil {
		t.Fatalf("construct generated output context: %v", err)
	}
	return context
}

func mustFixtureDecision(
	t testing.TB,
	kind RequestKind,
	target RequestTarget,
	depth uint64,
	groupID GroupID,
	rateScopeID RateScopeID,
	concurrency uint64,
	intervalMS uint64,
) PolicyDecision {
	t.Helper()
	decision, err := NewPolicyDecision(PolicyDecisionInput{
		RequestKind: kind, Target: target, Depth: depth, GroupID: groupID, RateScopeID: rateScopeID,
		GroupConcurrency: concurrency, GroupIntervalMS: intervalMS,
		OriginConcurrency: concurrency, OriginIntervalMS: intervalMS,
	})
	if err != nil {
		t.Fatalf("construct fixture policy decision: %v", err)
	}
	return decision
}

func mustFixtureTargetForURL(t testing.TB, canonicalURL string) RequestTarget {
	t.Helper()
	identity, err := utils.CanonicalizeURLV1(canonicalURL)
	if err != nil || identity.CanonicalURL != canonicalURL {
		t.Fatalf("generate canonical fixture URL: canonical=%q err=%v", identity.CanonicalURL, err)
	}
	jobID, err := ParseJobID(identity.URLID)
	if err != nil {
		t.Fatal(err)
	}
	return RequestTarget{URLID: jobID, CanonicalURL: canonicalURL}
}

func fixtureSizedURL(host, namespace string, index, size int) string {
	prefix := fmt.Sprintf("https://%s/%s/%05d/", host, namespace, index)
	if len(prefix) > size {
		panic("fixture URL prefix exceeds requested size")
	}
	return prefix + strings.Repeat("x", size-len(prefix))
}

func fixtureMaximumContentType() string {
	const prefix = "text/html;"
	const suffix = "charset=utf-8"
	return prefix + strings.Repeat(" ", 1024-len(prefix)-len(suffix)) + suffix
}

func cloneFixtureOutput(output CrawlOutput) CrawlOutput {
	clone := output
	clone.Page.HTML = append([]byte(nil), output.Page.HTML...)
	clone.Page.OriginalHTML = append([]byte(nil), output.Page.OriginalHTML...)
	clone.Outlinks = append([]string(nil), output.Outlinks...)
	clone.Images = append([]OutputImage(nil), output.Images...)
	clone.Discoveries = append([]OutputDiscovery(nil), output.Discoveries...)
	return clone
}

func reverseStrings(values []string) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func reverseImages(values []OutputImage) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func reverseDiscoveries(values []OutputDiscovery) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func (h *fixtureConformanceHarness) sourceProfile(t *testing.T, name string, generator *fixtureMaximumSourceGenerator) sourceProfileResult {
	t.Helper()
	generatorSHA256 := fixtureGeneratorSHA256(t, generator)
	if cached, ok := h.sourceProfiles[name]; ok {
		if h.sourceProfileGenerators[name] != generatorSHA256 {
			t.Fatalf("source profile %q was requested with conflicting declarative generators", name)
		}
		return cached
	}
	jobs := make([]SourceJob, 0)
	switch name {
	case "empty":
		if generator != nil {
			t.Fatal("empty source profile must not provide a generator")
		}
		jobs = []SourceJob{}
	case "maximum":
		if generator == nil || generator.Grammar != "maximum_source_v1" {
			t.Fatal("maximum source profile is missing the maximum_source_v1 generator")
		}
		group := fixtureGeneratedPolicyGroupValue(t, generator.PolicyGroup)
		if generator.Source.PolicyGroupRef != "policy_group" {
			t.Fatal("invalid maximum-source policy-group reference")
		}
		kind, err := ParseRequestKind(generator.Source.RequestKind)
		if err != nil || kind != RequestDocument {
			t.Fatalf("maximum-source request kind: %v", err)
		}
		score, err := ParseScoreText(generator.Source.ScoreText)
		if err != nil {
			t.Fatal(err)
		}
		urls := fixtureGeneratedURLs(t, generator.Jobs)
		if len(urls) != MaxJobsPerRun {
			t.Fatalf("maximum source generator count = %d, want %d", len(urls), MaxJobsPerRun)
		}
		jobs = make([]SourceJob, len(urls))
		for index, canonicalURL := range urls {
			target := mustFixtureTargetForURL(t, canonicalURL)
			decision := fixtureGeneratedDecision(t, kind, target, generator.Source.Depth, group, generator.PolicyGroup)
			jobs[index] = SourceJob{
				JobID: target.URLID, CanonicalURL: target.CanonicalURL, ScoreText: score,
				Depth: generator.Source.Depth, GroupID: group.GroupID, RateScopeID: group.RateScopeID, Decision: decision,
			}
		}
		authority := newAuthenticatedTestRunPolicyAuthority(
			t, RunID(strings.Repeat("1", 32)), Digest(strings.Repeat("a", 64)),
			plainSHA256(testDenyAllRenderPolicyArtifact()), []PolicyGroup{group},
		)
		if err := ValidateRunPinnedPolicyBindings(authority, RunPinnedPolicyBindings{
			SourceJobs: jobs,
		}); err != nil {
			t.Fatalf("maximum source policy binding: %v", err)
		}
	default:
		t.Fatalf("unsupported source profile %q", name)
	}
	ordered := append([]SourceJob(nil), jobs...)
	sort.Slice(ordered, func(left, right int) bool { return string(ordered[left].JobID) < string(ordered[right].JobID) })
	records := make([]Record, len(ordered))
	for index, job := range ordered {
		var err error
		records[index], err = sourceJobRecord(job)
		if err != nil {
			t.Fatalf("encode source profile record %d: %v", index, err)
		}
	}
	section, err := EncodeSection("jobs", records)
	if err != nil {
		t.Fatal(err)
	}
	assembled := digestEncoded("mifolyo:crawl-source:v2", section)
	apiDigest, err := DeriveSourceDigest(jobs)
	if err != nil {
		t.Fatalf("derive source profile digest: %v", err)
	}
	if assembled != apiDigest {
		t.Fatalf("assembled source digest %s differs from package API %s", assembled, apiDigest)
	}
	result := sourceProfileResult{Count: len(records), SectionSHA256: fixtureSHA256(section), SourceSHA256: string(apiDigest)}
	h.sourceProfiles[name] = result
	h.sourceProfileGenerators[name] = generatorSHA256
	return result
}

func (h *fixtureConformanceHarness) verifyPublicationIndependenceCase(t *testing.T, vector digestVectorCase) {
	t.Helper()
	input := decodeVectorPart[struct {
		AlternatePublicationID string `json:"alternate_publication_id"`
	}](t, vector.Input, vector.Name+".input")
	type publicationExpected struct {
		OutputDigest      string `json:"output_digest"`
		ProjectionASHA256 string `json:"projection_a_sha256"`
		ProjectionBSHA256 string `json:"projection_b_sha256"`
	}
	expected := decodeVectorPart[publicationExpected](t, vector.Expected, vector.Name+".expected")
	profile := h.outputProfile(t, "baseline")
	publicationA := mustFixtureDigest(t, profile.result.PublicationID)
	publicationB := mustFixtureDigest(t, input.AlternatePublicationID)
	digestA, projectionA := fixturePublicationProjection(t, profile, publicationA)
	digestB, projectionB := fixturePublicationProjection(t, profile, publicationB)
	if digestA != digestB {
		t.Fatalf("publication-derived records changed output digest: a=%s b=%s", digestA, digestB)
	}
	if projectionA == projectionB {
		t.Fatal("alternate publication did not change projection fingerprint")
	}
	actual := publicationExpected{
		OutputDigest: string(digestA), ProjectionASHA256: projectionA, ProjectionBSHA256: projectionB,
	}
	assertVectorValue(t, actual, expected)
}

func fixturePublicationProjection(t *testing.T, profile *fixtureOutputProfile, publicationID Digest) (Digest, string) {
	t.Helper()
	finalPage, err := NewFinalPageRecord(profile.context, profile.output.Page, publicationID)
	if err != nil {
		t.Fatal(err)
	}
	pageRecord, err := finalPage.Record()
	if err != nil {
		t.Fatal(err)
	}
	pageBytes, err := finalPage.Encode()
	if err != nil {
		t.Fatal(err)
	}
	projectionBytes := append([]byte(nil), pageBytes...)
	orderedImages := append([]OutputImage(nil), profile.output.Images...)
	sort.Slice(orderedImages, func(left, right int) bool {
		return orderedImages[left].NormalizedSourceURL < orderedImages[right].NormalizedSourceURL
	})
	for _, image := range orderedImages {
		finalImage, err := NewFinalImageRecord(publicationID, profile.output.Page.NormalizedURL, image)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := finalImage.Encode()
		if err != nil {
			t.Fatal(err)
		}
		projectionBytes = append(projectionBytes, encoded...)
	}
	manifest, err := NewImageManifestRecord(publicationID, profile.output.Page.NormalizedURL, profile.output.Images)
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := manifest.Encode()
	if err != nil {
		t.Fatal(err)
	}
	projectionBytes = append(projectionBytes, manifestBytes...)
	semanticPage, err := EncodeSection("page", []Record{cloneRecord(pageRecord[:9])})
	if err != nil {
		t.Fatal(err)
	}
	outlinks := mustEncodeFixtureSection(t, "outlinks", profile.sections.Outlinks)
	images := mustEncodeFixtureSection(t, "images", profile.sections.Images)
	discoveries := mustEncodeFixtureSection(t, "discoveries", profile.sections.Discoveries)
	aliases := mustEncodeFixtureSection(t, "aliases", profile.sections.Aliases)
	digest := digestEncoded("mifolyo:crawl-output:v2", semanticPage, outlinks, images, discoveries, aliases)
	apiDigest, err := DeriveOutputDigest(profile.context, profile.output)
	if err != nil || apiDigest != digest {
		t.Fatalf("projection semantic digest mismatch: computed=%s api=%s err=%v", digest, apiDigest, err)
	}
	return digest, fixtureSHA256(projectionBytes)
}

func TestFinalPageOutputAuthorityChecksRenderedDigestAndRule(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	harness := newFixtureConformanceHarness(t, fixture)
	profile := harness.outputProfile(t, "rendered")
	publicationID := mustFixtureDigest(t, profile.result.PublicationID)
	page, err := NewFinalPageRecord(profile.context, profile.output.Page, publicationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.ValidateAgainstContext(profile.context); err != nil {
		t.Fatalf("valid rendered page authority: %v", err)
	}

	fields, err := page.Record()
	if err != nil {
		t.Fatal(err)
	}
	fields[8].Value = []byte(strings.Repeat("f", 64))
	if string(fields[8].Value) == string(profile.context.renderPolicy.digest) {
		fields[8].Value = []byte(strings.Repeat("e", 64))
	}
	wrongDigest, err := newFinalPageRecord(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := wrongDigest.ValidateAgainstContext(profile.context); !errors.Is(err, ErrArtifactMismatch) {
		t.Fatalf("wrong rendered digest error = %v", err)
	}

	fields, err = page.Record()
	if err != nil {
		t.Fatal(err)
	}
	fields[7].Value = []byte("fabricated-render-rule")
	wrongRule, err := newFinalPageRecord(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := wrongRule.ValidateAgainstContext(profile.context); !errors.Is(err, ErrArtifactMismatch) {
		t.Fatalf("unauthorized rendered rule error = %v", err)
	}

	disabledContext := profile.context
	disabledContext.renderPolicy = cloneRenderPolicyAuthorization(profile.context.renderPolicy)
	disabledContext.renderPolicy.matcher = nil
	if err := page.ValidateAgainstContext(disabledContext); !errors.Is(err, ErrArtifactMismatch) {
		t.Fatalf("invalid render authorization error = %v", err)
	}
}

func mustEncodeFixtureSection(t testing.TB, label string, records []Record) []byte {
	t.Helper()
	encoded, err := EncodeSection(label, records)
	if err != nil {
		t.Fatalf("encode fixture section %q: %v", label, err)
	}
	return encoded
}

func (h *fixtureConformanceHarness) verifyFieldLimitsCase(t *testing.T, vector digestVectorCase) {
	t.Helper()
	validateFixtureOutputProfileInputShape(t, vector.Input, vector.Name+".input")
	input := decodeVectorPart[fixtureOutputProfileInput](t, vector.Input, vector.Name+".input")
	if input.Profile != "maximum" {
		t.Fatalf("unsupported field-limit profile %q", input.Profile)
	}
	type fieldLimitExpected struct {
		Lengths      map[string]int    `json:"lengths"`
		RecordSHA256 map[string]string `json:"record_sha256"`
	}
	expected := decodeVectorPart[fieldLimitExpected](t, vector.Expected, vector.Name+".expected")
	profile := h.outputProfileWithGenerator(t, "maximum", input.Generator)
	pageRecordBytes, err := EncodeRecord(profile.sections.Page[0])
	if err != nil {
		t.Fatal(err)
	}
	firstImageRecord := profile.sections.Images[0]
	firstImage := OutputImage{NormalizedSourceURL: string(firstImageRecord[0].Value), Alt: string(firstImageRecord[1].Value)}
	publicationID := mustFixtureDigest(t, profile.result.PublicationID)
	finalImage, err := NewFinalImageRecord(publicationID, profile.output.Page.NormalizedURL, firstImage)
	if err != nil {
		t.Fatal(err)
	}
	finalImageBytes, err := finalImage.Encode()
	if err != nil {
		t.Fatal(err)
	}
	sourceRecord, err := sourceJobRecord(profile.source)
	if err != nil {
		t.Fatal(err)
	}
	sourceRecordBytes, err := EncodeRecord(sourceRecord)
	if err != nil {
		t.Fatal(err)
	}
	manifestChunks := profile.chunks[string(ChunkImageManifest)]
	if len(manifestChunks) != 1 {
		t.Fatalf("maximum image manifest chunks = %d, want 1", len(manifestChunks))
	}
	manifestRecord := manifestChunks[0].Records()[0]
	manifestRecordBytes, err := EncodeRecord(manifestRecord)
	if err != nil {
		t.Fatal(err)
	}
	actual := fieldLimitExpected{
		Lengths: map[string]int{
			"canonical_url_bytes":      len(profile.output.Page.NormalizedURL),
			"html_bytes":               len(profile.output.Page.HTML),
			"original_html_bytes":      len(profile.output.Page.OriginalHTML),
			"combined_html_bytes":      len(profile.output.Page.HTML) + len(profile.output.Page.OriginalHTML),
			"content_type_bytes":       len(profile.output.Page.ContentType),
			"render_policy_rule_bytes": len(profile.output.Page.RenderPolicyRule),
			"image_alt_bytes":          len(firstImage.Alt),
			"group_id_bytes":           len(profile.source.GroupID),
			"outlink_chunk_records":    len(profile.chunks[string(ChunkOutlinks)][0].Records()),
			"discovery_chunk_records":  len(profile.chunks[string(ChunkDiscoveries)][0].Records()),
			"alias_records":            len(profile.chunks[string(ChunkAliases)][0].Records()),
			"image_chunk_records":      len(profile.chunks[string(ChunkImages)][0].Records()),
			"image_manifest_bytes":     len(manifestRecord[4].Value),
			"page_record_bytes":        len(pageRecordBytes),
			"final_image_record_bytes": len(finalImageBytes),
			"source_record_bytes":      len(sourceRecordBytes),
		},
		RecordSHA256: map[string]string{
			"page": fixtureSHA256(pageRecordBytes), "final_image": fixtureSHA256(finalImageBytes),
			"source": fixtureSHA256(sourceRecordBytes), "image_manifest": fixtureSHA256(manifestRecordBytes),
		},
	}
	assertVectorValue(t, actual, expected)
}
