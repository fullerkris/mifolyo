package main

import (
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestBuildGraphUsesMetadataNodesAndCurrentInternalOutlinks(t *testing.T) {
	t.Parallel()

	nodes := []string{
		"https://e.test/",
		"https://c.test/",
		"https://a.test/",
		"https://d.test/",
		"https://b.test/",
	}
	outlinks := []outlinkDocument{
		{ID: "https://d.test/", Links: []string{"https://outside.test/"}},
		{ID: "https://c.test/", Links: []string{"https://a.test/"}},
		{ID: "https://orphan.test/", Links: []string{"https://a.test/"}},
		{ID: "https://b.test/", Links: []string{"https://c.test/"}},
		{ID: "https://a.test/", Links: []string{
			"https://b.test/",
			"https://b.test/",
			"https://outside.test/",
		}},
	}

	result, err := buildGraph(nodes, outlinks)
	if err != nil {
		t.Fatalf("buildGraph() error = %v", err)
	}
	if !slices.Equal(result.Nodes, []string{
		"https://a.test/",
		"https://b.test/",
		"https://c.test/",
		"https://d.test/",
		"https://e.test/",
	}) {
		t.Fatalf("unexpected sorted nodes: %#v", result.Nodes)
	}
	wantStats := graphStats{
		NodeCount:               5,
		InternalEdgeCount:       3,
		FilteredTargetCount:     2,
		DuplicateEdgeCount:      1,
		IgnoredSourceCount:      1,
		MissingOutlinksCount:    1,
		DanglingNodeCount:       2,
		RawOutlinkDocumentCount: 5,
	}
	if result.Stats != wantStats {
		t.Fatalf("graph stats = %#v, want %#v", result.Stats, wantStats)
	}
	if !slices.Equal(result.Outgoing[0], []int{1}) ||
		!slices.Equal(result.Outgoing[1], []int{2}) ||
		!slices.Equal(result.Outgoing[2], []int{0}) ||
		len(result.Outgoing[3]) != 0 || len(result.Outgoing[4]) != 0 {
		t.Fatalf("unexpected adjacency: %#v", result.Outgoing)
	}

	reversedNodes := append([]string(nil), nodes...)
	slices.Reverse(reversedNodes)
	reversedOutlinks := append([]outlinkDocument(nil), outlinks...)
	slices.Reverse(reversedOutlinks)
	reordered, err := buildGraph(reversedNodes, reversedOutlinks)
	if err != nil {
		t.Fatalf("buildGraph(reordered) error = %v", err)
	}
	if result.SHA256 != reordered.SHA256 {
		t.Fatalf("graph hash depends on input order: %s != %s", result.SHA256, reordered.SHA256)
	}
}

func TestCalculatePageRankRedistributesDanglingMass(t *testing.T) {
	t.Parallel()

	input, err := buildGraph(
		[]string{
			"https://a.test/",
			"https://b.test/",
			"https://c.test/",
			"https://d.test/",
			"https://e.test/",
		},
		[]outlinkDocument{
			{ID: "https://a.test/", Links: []string{"https://b.test/", "https://outside.test/"}},
			{ID: "https://b.test/", Links: []string{"https://c.test/"}},
			{ID: "https://c.test/", Links: []string{"https://a.test/"}},
			{ID: "https://d.test/", Links: []string{"https://outside.test/"}},
		},
	)
	if err != nil {
		t.Fatalf("buildGraph() error = %v", err)
	}

	result, err := calculatePageRank(input)
	if err != nil {
		t.Fatalf("calculatePageRank() error = %v", err)
	}
	want := []float64{
		0.303030303030303,
		0.303030303030303,
		0.303030303030303,
		0.0454545454545455,
		0.0454545454545455,
	}
	for index := range want {
		if math.Abs(result.Ranks[index]-want[index]) > 1e-10 {
			t.Errorf("rank[%d] = %.15f, want %.15f", index, result.Ranks[index], want[index])
		}
	}
	if math.Abs(result.Sum-1) > pageRankTolerance {
		t.Fatalf("rank sum = %.17g", result.Sum)
	}
	if result.Residual > pageRankTolerance {
		t.Fatalf("stationary residual = %.17g", result.Residual)
	}
}

func TestCalculatePageRankAllDanglingNodesAreUniform(t *testing.T) {
	t.Parallel()

	input, err := buildGraph(
		[]string{"https://a.test/", "https://b.test/"},
		nil,
	)
	if err != nil {
		t.Fatalf("buildGraph() error = %v", err)
	}
	result, err := calculatePageRank(input)
	if err != nil {
		t.Fatalf("calculatePageRank() error = %v", err)
	}
	if !slices.Equal(result.Ranks, []float64{0.5, 0.5}) {
		t.Fatalf("ranks = %#v, want uniform ranks", result.Ranks)
	}
	if result.Iterations != 1 || result.Residual != 0 || result.Sum != 1 {
		t.Fatalf("unexpected uniform result: %#v", result)
	}
}

func TestCalculatePageRankFailsOnNonConvergence(t *testing.T) {
	t.Parallel()

	input, err := buildGraph(
		[]string{"https://a.test/", "https://b.test/", "https://c.test/"},
		[]outlinkDocument{
			{ID: "https://a.test/", Links: []string{"https://b.test/"}},
			{ID: "https://b.test/", Links: []string{"https://a.test/"}},
		},
	)
	if err != nil {
		t.Fatalf("buildGraph() error = %v", err)
	}
	_, err = calculatePageRankWithParameters(input, pageRankDamping, 1e-30, 1)
	if err == nil || !strings.Contains(err.Error(), "did not converge") {
		t.Fatalf("error = %v, want non-convergence", err)
	}
}

func TestBuildGraphRejectsInvalidURLIdentity(t *testing.T) {
	t.Parallel()

	sensitive := "not-a-url?token=do-not-log"
	_, err := buildGraph([]string{sensitive}, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid metadata URL") {
		t.Fatalf("error = %v, want invalid metadata URL", err)
	}
	if strings.Contains(err.Error(), sensitive) || !strings.Contains(err.Error(), valueReference(sensitive)) {
		t.Fatalf("invalid URL error did not use a stable reference: %v", err)
	}
}

func TestBuildGraphErrorsReferenceRatherThanExposeURLs(t *testing.T) {
	t.Parallel()

	duplicate := "https://example.test/private/path?token=do-not-log"
	_, err := buildGraph([]string{duplicate, duplicate}, nil)
	if err == nil || strings.Contains(err.Error(), duplicate) || !strings.Contains(err.Error(), valueReference(duplicate)) {
		t.Fatalf("duplicate metadata error leaked URL or omitted reference: %v", err)
	}

	source := "https://source.test/private/"
	target := "not-a-url?content=do-not-log"
	_, err = buildGraph(
		[]string{source},
		[]outlinkDocument{{ID: source, Links: []string{target}}},
	)
	if err == nil || strings.Contains(err.Error(), source) || strings.Contains(err.Error(), target) {
		t.Fatalf("outlink error leaked source or target URL: %v", err)
	}
	if !strings.Contains(err.Error(), valueReference(source)) || !strings.Contains(err.Error(), valueReference(target)) {
		t.Fatalf("outlink error omitted stable references: %v", err)
	}
}

func TestOperationalErrorsRedactCauseAndPreserveWrapping(t *testing.T) {
	t.Parallel()

	cause := errors.New("database error includes https://example.test/private and document content")
	err := wrapOperationalError("pagerank: read metadata", cause)
	if !errors.Is(err, cause) {
		t.Fatal("operational error does not preserve its cause")
	}
	if strings.Contains(err.Error(), "example.test") || err.Error() != "pagerank: read metadata" {
		t.Fatalf("operational error exposed its cause: %v", err)
	}

	unknown := &activationOutcomeUnknownError{cause: cause}
	if !errors.Is(unknown, cause) {
		t.Fatal("activation error does not preserve its cause")
	}
	if strings.Contains(unknown.Error(), "example.test") {
		t.Fatalf("activation error exposed its cause: %v", unknown)
	}
}

func TestParseCommandOptionsRequiresExplicitValidatedHash(t *testing.T) {
	t.Parallel()

	if _, err := parseCommandOptions([]string{"--publish"}); err == nil {
		t.Fatal("parseCommandOptions() accepted publish without a graph hash")
	}
	options, err := parseCommandOptions([]string{
		"--publish",
		"--expected-graph-sha256=" + strings.Repeat("a", 64),
		"--confirm-target=mongo:27017/mifolyo_index/pagerank",
	})
	if err != nil {
		t.Fatalf("parseCommandOptions() error = %v", err)
	}
	if !options.Publish || options.ExpectedGraphSHA256 != strings.Repeat("a", 64) ||
		options.ConfirmTarget != "mongo:27017/mifolyo_index/pagerank" {
		t.Fatalf("unexpected options: %#v", options)
	}
}

func TestDescendingRankIndexRequiresExactKey(t *testing.T) {
	t.Parallel()

	if !isDescendingRankIndex(bson.D{{Key: "rank", Value: int32(-1)}}) {
		t.Fatal("isDescendingRankIndex() rejected the required index")
	}
	for _, key := range []bson.D{
		{{Key: "rank", Value: int32(1)}},
		{{Key: "other", Value: int32(-1)}},
		{{Key: "rank", Value: int32(-1)}, {Key: "_id", Value: int32(1)}},
	} {
		if isDescendingRankIndex(key) {
			t.Fatalf("isDescendingRankIndex() accepted %#v", key)
		}
	}
}

func TestValidatePublicationEnvironmentRequiresExactTarget(t *testing.T) {
	t.Setenv("MONGO_HOST", "mongo")
	t.Setenv("MONGO_PORT", "27017")
	t.Setenv("MONGO_DB", "mifolyo_index")

	valid := commandOptions{ConfirmTarget: "mongo:27017/mifolyo_index/pagerank"}
	if err := validatePublicationEnvironment(valid); err != nil {
		t.Fatalf("validatePublicationEnvironment() error = %v", err)
	}
	if err := validatePublicationEnvironment(commandOptions{ConfirmTarget: "localhost:27017/mifolyo_index/pagerank"}); err == nil {
		t.Fatal("validatePublicationEnvironment() accepted a different target")
	}
	t.Setenv("MONGO_DB", "admin")
	if err := validatePublicationEnvironment(valid); err == nil {
		t.Fatal("validatePublicationEnvironment() accepted a system database")
	}
}

func TestValidateMongoAuthenticationFailsClosed(t *testing.T) {
	t.Parallel()

	if err := validateMongoAuthentication("", "", false); err == nil {
		t.Fatal("validateMongoAuthentication() accepted unauthenticated MongoDB without opt-in")
	}
	if err := validateMongoAuthentication("", "", true); err != nil {
		t.Fatalf("validateMongoAuthentication() rejected explicit local opt-in: %v", err)
	}
	if err := validateMongoAuthentication("user", "password", false); err != nil {
		t.Fatalf("validateMongoAuthentication() rejected paired credentials: %v", err)
	}
	for _, credentials := range [][2]string{{"user", ""}, {"", "password"}} {
		if err := validateMongoAuthentication(credentials[0], credentials[1], true); err == nil {
			t.Fatalf("validateMongoAuthentication() accepted unpaired credentials: %#v", credentials)
		}
	}
}
