package crawljobsv2

import (
	"fmt"
	"sort"
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
	context  OutputContext
	output   CrawlOutput
	source   SourceJob
	sections fixtureOutputSections
	chunks   map[string][]StageChunk
	result   outputProfileExpected
}

type sourceProfileResult struct {
	Count         int    `json:"count"`
	SectionSHA256 string `json:"section_sha256"`
	SourceSHA256  string `json:"source_sha256"`
}

func (h *fixtureConformanceHarness) verifyOutputProfileCase(t *testing.T, vector digestVectorCase) {
	t.Helper()
	input := decodeVectorPart[struct {
		Profile string `json:"profile"`
	}](t, vector.Input, vector.Name+".input")
	expected := decodeVectorPart[outputProfileExpected](t, vector.Expected, vector.Name+".expected")
	actual := h.outputProfile(t, input.Profile).result
	assertVectorValue(t, actual, expected)
}

func (h *fixtureConformanceHarness) verifySourceProfileCase(t *testing.T, vector digestVectorCase) {
	t.Helper()
	input := decodeVectorPart[struct {
		Profile string `json:"profile"`
	}](t, vector.Input, vector.Name+".input")
	expected := decodeVectorPart[sourceProfileResult](t, vector.Expected, vector.Name+".expected")
	actual := h.sourceProfile(t, input.Profile)
	assertVectorValue(t, actual, expected)
}

func (h *fixtureConformanceHarness) verifyStageChunksCase(t *testing.T, vector digestVectorCase) {
	t.Helper()
	input := decodeVectorPart[struct {
		Profile string `json:"profile"`
	}](t, vector.Input, vector.Name+".input")
	expected := decodeVectorPart[struct {
		ChunkDigests map[string][]string `json:"chunk_digests"`
	}](t, vector.Expected, vector.Name+".expected")
	actual := struct {
		ChunkDigests map[string][]string `json:"chunk_digests"`
	}{ChunkDigests: h.outputProfile(t, input.Profile).result.ChunkDigests}
	assertVectorValue(t, actual, expected)
}

func (h *fixtureConformanceHarness) outputProfile(t *testing.T, name string) *fixtureOutputProfile {
	t.Helper()
	if cached, ok := h.outputProfiles[name]; ok {
		return cached
	}

	var context OutputContext
	var output CrawlOutput
	var source SourceJob
	var lease LeaseIdentity
	switch name {
	case "baseline":
		context = vectorOutputContextValue(t, h.fixture)
		output = vectorOutputValue(t, h.fixture)
		source = vectorSourceJobValue(t, h.fixture, h.fixture.OutputContext.SourceJobIndex)
		lease = vectorLease(t, h.fixture)
	case "empty":
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
		context = vectorOutputContextWith(t, h.fixture, len(h.fixture.OutputContext.Requests), []string{"render-main"})
		output = cloneFixtureOutput(vectorOutputValue(t, h.fixture))
		output.Page.HTML = []byte("<html>rendered</html>")
		output.Page.OriginalHTML = []byte("<html>source</html>")
		output.Page.Rendered = true
		output.Page.RenderPolicyRule = "render-main"
		output.Page.RenderPolicyDigest = mustFixtureDigest(t, h.fixture.Identities.CrawlPolicyDigest)
		source = vectorSourceJobValue(t, h.fixture, h.fixture.OutputContext.SourceJobIndex)
		lease = vectorLease(t, h.fixture)
	case "maximum":
		context, output, source, lease = h.buildMaximumOutput(t)
	default:
		t.Fatalf("unsupported output profile %q", name)
	}

	profile := buildFixtureOutputProfile(t, context, output, source, lease)
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
	if !context.initialized || context.jobID != source.JobID || lease.JobID != source.JobID || context.leaseFence != lease.Fence {
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
	add(NewPageFieldsStageChunk(commitID, publicationID, context, output.Page))
	add(NewPageBlobStageChunk(commitID, ChunkHTML, output.Page.HTML))
	add(NewPageBlobStageChunk(commitID, ChunkOriginalHTML, output.Page.OriginalHTML))

	orderedOutlinks := append([]string(nil), output.Outlinks...)
	sort.Strings(orderedOutlinks)
	for first := 0; first < len(orderedOutlinks); first += MaxNonBlobStageBatchRecords {
		last := first + MaxNonBlobStageBatchRecords
		if last > len(orderedOutlinks) {
			last = len(orderedOutlinks)
		}
		add(NewOutlinksStageChunk(commitID, uint64(first/MaxNonBlobStageBatchRecords), context, orderedOutlinks[first:last]))
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
		add(NewDiscoveriesStageChunk(commitID, uint64(first/MaxNonBlobStageBatchRecords), orderedDiscoveries[first:last]))
	}
	add(NewAliasesStageChunk(commitID, context))
	if len(output.Images) != 0 {
		add(NewImagesStageChunk(commitID, output.Images))
	}
	add(NewImageManifestStageChunk(commitID, publicationID, context, output.Images))
	return chunks, digests
}

func (h *fixtureConformanceHarness) buildMaximumOutput(t *testing.T) (OutputContext, CrawlOutput, SourceJob, LeaseIdentity) {
	t.Helper()
	groupID, err := ParseGroupID(strings.Repeat("g", MaxPolicyGroupIDBytes))
	if err != nil {
		t.Fatal(err)
	}
	rateScopeID, err := ParseRateScopeID(strings.Repeat("3", 32))
	if err != nil {
		t.Fatal(err)
	}
	aliasTargets := make([]RequestTarget, MaxAliasesPerJob)
	for index := range aliasTargets {
		aliasTargets[index] = mustFixtureTargetForURL(t, fixtureSizedURL("aliases.example.com", "alias", index, MaxCanonicalURLBytes))
	}
	sourceDecision := mustFixtureDecision(t, RequestDocument, aliasTargets[0], MaxExactInteger, groupID, rateScopeID, 32, 3_600_000)
	score, err := ParseScoreText("-1000")
	if err != nil {
		t.Fatal(err)
	}
	source := SourceJob{
		JobID: aliasTargets[0].URLID, CanonicalURL: aliasTargets[0].CanonicalURL, ScoreText: score,
		Depth: MaxExactInteger, GroupID: groupID, RateScopeID: rateScopeID, Decision: sourceDecision,
	}
	lease := vectorLease(t, h.fixture)
	lease.JobID = source.JobID
	policyDigest := mustFixtureDigest(t, h.fixture.Identities.CrawlPolicyDigest)
	startedAt := make([]uint64, len(aliasTargets))
	for index := range startedAt {
		startedAt[index] = 1_788_266_095_000 + uint64(index)*1_000
	}
	renderRule := strings.Repeat("r", MaxRenderPolicyRuleIDBytes)
	context := buildOpaqueFixtureOutputContext(t, source, lease, aliasTargets, startedAt, policyDigest, []string{renderRule})

	outlinks := make([]string, MaxOutlinksPerJob)
	for index := range outlinks {
		outlinks[index] = fixtureSizedURL("outlinks.example.com", "outlink", index, MaxCanonicalURLBytes)
	}
	reverseStrings(outlinks)
	images := make([]OutputImage, MaxImagesPerPage)
	for index := range images {
		images[index] = OutputImage{
			NormalizedSourceURL: fixtureSizedURL("images.example.com", "image", index, MaxCanonicalURLBytes),
			Alt:                 strings.Repeat("é", MaxImageAltBytes/2),
		}
	}
	reverseImages(images)
	discoveries := make([]OutputDiscovery, MaxDiscoveriesPerJob)
	discoveryScore, err := ParseScoreText("10000")
	if err != nil {
		t.Fatal(err)
	}
	for index := range discoveries {
		target := mustFixtureTargetForURL(t, fixtureSizedURL("discoveries.example.com", "discovery", index, MaxCanonicalURLBytes))
		decision := mustFixtureDecision(t, RequestDocument, target, MaxExactInteger, groupID, rateScopeID, 32, 3_600_000)
		discoveries[index] = OutputDiscovery{
			JobID: target.URLID, CanonicalURL: target.CanonicalURL, Depth: MaxExactInteger,
			ScoreText: discoveryScore, GroupID: groupID, RateScopeID: rateScopeID, Decision: decision,
		}
	}
	reverseDiscoveries(discoveries)
	output := CrawlOutput{
		Page: OutputPage{
			NormalizedURL: aliasTargets[len(aliasTargets)-1].CanonicalURL,
			HTML:          []byte(strings.Repeat("H", MaxPageBlobBytes)), OriginalHTML: []byte(strings.Repeat("O", MaxPageBlobBytes)),
			ContentType: fixtureMaximumContentType(), StatusCode: 399, Rendered: true,
			RenderPolicyRule: renderRule, RenderPolicyDigest: policyDigest,
		},
		Outlinks: outlinks, Images: images, Discoveries: discoveries,
	}
	return context, output, source, lease
}

func buildOpaqueFixtureOutputContext(
	t *testing.T,
	source SourceJob,
	lease LeaseIdentity,
	targets []RequestTarget,
	startedAt []uint64,
	policyDigest Digest,
	enabledRenderRules []string,
) OutputContext {
	t.Helper()
	if len(targets) == 0 || len(targets) != len(startedAt) {
		t.Fatal("invalid generated request chain")
	}
	var transcript DocumentTranscript
	for index, target := range targets {
		kind := RequestRedirect
		if index == 0 {
			kind = RequestDocument
		}
		decision := mustFixtureDecision(
			t, kind, target, source.Depth, source.GroupID, source.RateScopeID,
			source.Decision.GroupConcurrency, source.Decision.GroupIntervalMS,
		)
		intent := ReservationIntent{
			Lease: lease, RequestOrdinal: uint64(index + 1), Target: target,
			CrawlPolicyDigest: policyDigest, Decision: decision,
		}
		reservationID, err := DeriveReservationID(intent)
		if err != nil {
			t.Fatalf("derive generated reservation: %v", err)
		}
		starts := uint64(index + 1)
		response, err := ParseStartRequestResponse(intent, []string{
			string(StatusStarted), canonicalDecimal(startedAt[index]), string(reservationID),
			canonicalDecimal(startedAt[index]), "1", canonicalDecimal(starts),
			canonicalDecimal(starts), canonicalDecimal(starts), "1",
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
			transcript, err = NewDocumentTranscript(source, evidence)
		} else {
			transcript, err = transcript.AppendRedirect(evidence)
		}
		if err != nil {
			t.Fatalf("append generated request evidence: %v", err)
		}
	}
	finalTarget := targets[len(targets)-1]
	finalDigest, err := DeriveTargetDigest(finalTarget)
	if err != nil {
		t.Fatal(err)
	}
	witness, err := ParseFinalDocumentWitness(lease, []string{
		canonicalDecimal(startedAt[len(startedAt)-1]), canonicalDecimal(uint64(lease.Fence)),
		string(finalTarget.URLID), finalTarget.CanonicalURL, string(finalDigest),
	})
	if err != nil {
		t.Fatalf("parse generated final witness: %v", err)
	}
	authorization, err := NewRenderPolicyAuthorization(policyDigest, policyDigest, enabledRenderRules)
	if err != nil {
		t.Fatalf("construct generated render authorization: %v", err)
	}
	context, err := NewOutputContext(source, transcript, witness, authorization)
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

func (h *fixtureConformanceHarness) sourceProfile(t *testing.T, name string) sourceProfileResult {
	t.Helper()
	if cached, ok := h.sourceProfiles[name]; ok {
		return cached
	}
	jobs := make([]SourceJob, 0)
	switch name {
	case "empty":
		jobs = []SourceJob{}
	case "maximum":
		groupID, err := ParseGroupID("source-group")
		if err != nil {
			t.Fatal(err)
		}
		rateScopeID, err := ParseRateScopeID(strings.Repeat("4", 32))
		if err != nil {
			t.Fatal(err)
		}
		score, err := ParseScoreText("0")
		if err != nil {
			t.Fatal(err)
		}
		jobs = make([]SourceJob, MaxJobsPerRun)
		for index := range jobs {
			target := mustFixtureTargetForURL(t, fmt.Sprintf("https://sources.example.com/job/%05d", index))
			decision := mustFixtureDecision(t, RequestDocument, target, MaxExactInteger, groupID, rateScopeID, 32, 3_600_000)
			jobs[index] = SourceJob{
				JobID: target.URLID, CanonicalURL: target.CanonicalURL, ScoreText: score,
				Depth: MaxExactInteger, GroupID: groupID, RateScopeID: rateScopeID, Decision: decision,
			}
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
	input := decodeVectorPart[struct {
		Profile string `json:"profile"`
	}](t, vector.Input, vector.Name+".input")
	if input.Profile != "maximum" {
		t.Fatalf("unsupported field-limit profile %q", input.Profile)
	}
	type fieldLimitExpected struct {
		Lengths      map[string]int    `json:"lengths"`
		RecordSHA256 map[string]string `json:"record_sha256"`
	}
	expected := decodeVectorPart[fieldLimitExpected](t, vector.Expected, vector.Name+".expected")
	profile := h.outputProfile(t, "maximum")
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
