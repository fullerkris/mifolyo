package crawljobsv2

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// Every generated job is valid and unique independently of the aggregate cap.
// Removing a count guard must not merely reveal malformed or duplicate input.
func fixtureCountSourceJobs(t *testing.T, prototype SourceJob, count int) []SourceJob {
	t.Helper()
	if count < 0 || count > MaxJobsPerRun+1 {
		t.Fatal("unsupported fixture source count")
	}
	jobs := make([]SourceJob, count)
	seen := make(map[JobID]struct{}, count)
	for index := range jobs {
		target := mustFixtureTargetForURL(t, fmtURL("count-sources.example.com", index))
		job := prototype
		job.JobID, job.CanonicalURL = target.URLID, target.CanonicalURL
		job.Decision = mustFixtureDecision(
			t, RequestDocument, target, job.Depth, job.GroupID, job.RateScopeID,
			prototype.Decision.GroupConcurrency, prototype.Decision.GroupIntervalMS,
		)
		if _, err := sourceJobRecord(job); err != nil {
			t.Fatalf("count fixture job %d is otherwise invalid: %v", index, err)
		}
		if _, duplicate := seen[job.JobID]; duplicate {
			t.Fatalf("count fixture job %d duplicates another generated identity", index)
		}
		seen[job.JobID] = struct{}{}
		jobs[index] = job
	}
	return jobs
}

func fixtureCountDiscoveries(t *testing.T, context OutputContext, prototype OutputDiscovery, count int) []OutputDiscovery {
	t.Helper()
	jobs := fixtureCountSourceJobs(t, SourceJob{
		JobID: prototype.JobID, CanonicalURL: prototype.CanonicalURL, ScoreText: prototype.ScoreText,
		Depth: prototype.Depth, GroupID: prototype.GroupID, RateScopeID: prototype.RateScopeID, Decision: prototype.Decision,
	}, count)
	values := make([]OutputDiscovery, count)
	for index, job := range jobs {
		values[index] = OutputDiscovery{
			JobID: job.JobID, CanonicalURL: job.CanonicalURL, ScoreText: job.ScoreText,
			Depth: job.Depth, GroupID: job.GroupID, RateScopeID: job.RateScopeID, Decision: job.Decision,
		}
	}
	if err := validateOutputDiscoveriesAgainstRunPolicy(context.runPolicy, values); err != nil {
		t.Fatalf("count fixture discoveries are not otherwise policy-valid: %v", err)
	}
	return values
}

// A semantically valid manifest cannot reach 384 KiB under the URL/image caps.
// Large sizes deliberately encode an overlong source URL in compact JSON. The
// size guard must return ErrInvalidChunk before URL validation; without it the
// distinct ErrInvalidCanonicalURL must fail the negative oracle, not mask it.
func fixtureSizedImageManifestRecord(t *testing.T, publicationID Digest, pageURL string, size int) Record {
	t.Helper()
	const sourcePrefix = "https://images.example.com/"
	key, err := ImageDataKey(publicationID, pageURL, sourcePrefix)
	if err != nil {
		t.Fatal(err)
	}
	prefix := key[:strings.LastIndexByte(key, ':')+1]
	sourceBytes := (size - len(prefix) - len(`[""]`)) * 3 / 4
	if sourceBytes < len(sourcePrefix) {
		t.Fatal("manifest fixture size is too small for its source URL")
	}
	sourceURL := sourcePrefix + strings.Repeat("x", sourceBytes-len(sourcePrefix))
	imageKeys, err := json.Marshal([]string{prefix + base64.RawURLEncoding.EncodeToString([]byte(sourceURL))})
	if err != nil || len(imageKeys) != size {
		t.Fatalf("manifest fixture width = %d, want %d: %v", len(imageKeys), size, err)
	}
	return Record{
		textField("contract_version", "1"),
		textField("publication_id", string(publicationID)),
		textField("normalized_url", pageURL),
		textField("image_count", "1"),
		{Name: "image_keys", Value: imageKeys},
	}
}

func TestFixtureSourceCountCorpusAndExactErrors(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	jobs := fixtureCountSourceJobs(t, vectorSourceJobValue(t, fixture, 0), MaxJobsPerRun+1)
	for _, valid := range [][]SourceJob{jobs[:MaxJobsPerRun], jobs[MaxJobsPerRun:]} {
		if _, err := DeriveSourceDigest(valid); err != nil {
			t.Fatalf("otherwise-valid source-count control rejected: %v", err)
		}
	}
	if _, err := DeriveSourceDigest(jobs); err != ErrInputLimitExceeded {
		t.Fatalf("10001 valid unique jobs: error = %v; want exact ErrInputLimitExceeded", err)
	}
	for _, test := range []struct {
		name   string
		mutate func([]SourceJob)
		want   error
	}{
		{"malformed", func(jobs []SourceJob) { jobs[1] = SourceJob{} }, ErrInvalidJobID},
		{"duplicate", func(jobs []SourceJob) { jobs[1] = jobs[0] }, ErrDuplicateSourceJob},
		{"policy_mismatch", func(jobs []SourceJob) { jobs[1].Decision.Depth++ }, ErrDigestInputMismatch},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := append([]SourceJob(nil), jobs[:2]...)
			test.mutate(changed)
			if _, err := DeriveSourceDigest(changed); err != test.want {
				t.Fatalf("in-range %s error = %v; want exact %v, not source count evidence", test.name, err, test.want)
			}
		})
	}
}

func TestFixtureSectionCountsUseProductionSemanticErrors(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	for _, test := range []struct {
		section string
		maximum int
		class   string
	}{
		{"outlinks", MaxOutlinksPerJob, "OUTPUT_COUNT_LIMIT"},
		{"images", MaxImagesPerPage, "OUTPUT_COUNT_LIMIT"},
		{"discoveries", MaxDiscoveriesPerJob, "OUTPUT_COUNT_LIMIT"},
		{"aliases", MaxAliasesPerJob, "ALIAS_COUNT_LIMIT"},
	} {
		t.Run(test.section, func(t *testing.T) {
			if class := fixtureSectionShapeRejection(t, fixture, test.section, test.maximum); class != "" {
				t.Fatalf("valid maximum-size section rejected as %s", class)
			}
			if class := fixtureSectionShapeRejection(t, fixture, test.section, test.maximum+1); class != test.class {
				t.Fatalf("one-over section class = %q, want %q", class, test.class)
			}
		})
	}
}

func TestFixturePageSectionCountsAreGrammarOnly(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	page, err := outputPageRecord(vectorOutputContextValue(t, fixture), vectorOutputValue(t, fixture).Page)
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{0, 2} {
		pages := make([]Record, count)
		for index := range pages {
			pages[index] = page
		}
		if _, err := EncodeSection("page", pages); err != nil {
			t.Fatalf("generic framing must not claim semantic page-count validation: %v", err)
		}
		if class := fixtureSectionShapeRejection(t, fixture, "page", count); class != "PAGE_SECTION_SHAPE" {
			t.Fatalf("grammar-only page count class = %q", class)
		}
	}
}

func TestFixtureStageChunkCountAndContentControls(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	harness := newFixtureConformanceHarness(t, fixture)
	profile := harness.outputProfile(t, "baseline")
	identity := outputCommitIdentity(profile.context, mustFixtureDigest(t, profile.result.PublicationID))
	commitID := mustFixtureDigest(t, profile.result.CommitID)
	t.Run("outlinks", func(t *testing.T) {
		values := make([]string, MaxNonBlobStageBatchRecords+1)
		for index := range values {
			values[index] = fmtURL("chunks.example.com", index)
		}
		if _, err := NewOutlinksStageChunk(identity, 0, profile.context, values[:MaxNonBlobStageBatchRecords]); err != nil {
			t.Fatalf("64 valid outlinks rejected: %v", err)
		}
		if _, err := NewOutlinksStageChunk(identity, 0, profile.context, values[MaxNonBlobStageBatchRecords:]); err != nil {
			t.Fatalf("extra valid outlink rejected individually: %v", err)
		}
		if _, err := NewOutlinksStageChunk(identity, 0, profile.context, []string{values[0], values[0]}); err != ErrDuplicateOutlink {
			t.Fatalf("in-range duplicate outlink error = %v; want exact ErrDuplicateOutlink, not count evidence", err)
		}
		for _, mutation := range []string{"outlinks_65_records", "outlinks_empty"} {
			if class := harness.fixtureStageMutationRejection(t, mutation); class != "CHUNK_RECORD_COUNT_LIMIT" {
				t.Fatalf("%s class = %q", mutation, class)
			}
		}
	})
	t.Run("discoveries", func(t *testing.T) {
		values := fixtureCountDiscoveries(t, profile.context, profile.output.Discoveries[0], MaxNonBlobStageBatchRecords+1)
		for _, valid := range [][]OutputDiscovery{values[:MaxNonBlobStageBatchRecords], values[MaxNonBlobStageBatchRecords:]} {
			if _, err := NewDiscoveriesStageChunk(identity, 0, profile.context, valid); err != nil {
				t.Fatalf("otherwise-valid discovery count control rejected: %v", err)
			}
		}
		if _, err := NewDiscoveriesStageChunk(identity, 0, profile.context, []OutputDiscovery{values[0], values[0]}); err != ErrDuplicateDiscovery {
			t.Fatalf("in-range duplicate discovery error = %v; want exact ErrDuplicateDiscovery, not count evidence", err)
		}
		changed := append([]OutputDiscovery(nil), values[:MaxNonBlobStageBatchRecords]...)
		changed[0].Decision.Depth++
		if _, err := NewDiscoveriesStageChunk(identity, 0, profile.context, changed); err != ErrDigestInputMismatch {
			t.Fatalf("in-range discovery policy error = %v; want exact ErrDigestInputMismatch, not count evidence", err)
		}
		if class := harness.fixtureStageMutationRejection(t, "discoveries_65_records"); class != "CHUNK_RECORD_COUNT_LIMIT" {
			t.Fatalf("discovery count class = %q", class)
		}
	})
	t.Run("blob", func(t *testing.T) {
		if _, err := NewPageBlobStageChunk(identity, profile.context, ChunkHTML, bytes.Repeat([]byte{'H'}, MaxPageBlobBytes)); err != nil {
			t.Fatalf("valid maximum-size UTF-8 blob rejected: %v", err)
		}
		if class := harness.fixtureStageMutationRejection(t, "blob_one_over"); class != "PAGE_BLOB_LIMIT" {
			t.Fatalf("blob size class = %q", class)
		}
	})
	t.Run("manifest", func(t *testing.T) {
		for _, test := range []struct {
			size int
			want error
		}{
			{512, nil},
			{MaxImageManifestBytes, ErrInvalidCanonicalURL},
			{MaxImageManifestBytes + 2, ErrInvalidChunk},
		} {
			record := fixtureSizedImageManifestRecord(t, identity.PublicationID, profile.output.Page.NormalizedURL, test.size)
			if _, err := newValidatedStageChunk(commitID, ChunkImageManifest, 0, []Record{record}); err != test.want {
				t.Fatalf("compact manifest width %d error = %v; want exact %v", test.size, err, test.want)
			}
		}
		if class := harness.fixtureStageMutationRejection(t, "image_manifest_one_over"); class != "IMAGE_MANIFEST_LIMIT" {
			t.Fatalf("manifest size class = %q", class)
		}
	})
}
