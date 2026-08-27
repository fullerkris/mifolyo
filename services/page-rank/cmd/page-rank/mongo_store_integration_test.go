package main

import (
	"context"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestPublishPageRankIntegration(t *testing.T) {
	uri := os.Getenv("PAGERANK_MONGO_INTEGRATION_URI")
	if uri == "" {
		t.Skip("PAGERANK_MONGO_INTEGRATION_URI is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("mongo.Connect() error = %v", err)
	}
	defer client.Disconnect(context.Background())
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatalf("client.Ping() error = %v", err)
	}

	suffix, err := randomHex(8)
	if err != nil {
		t.Fatalf("randomHex() error = %v", err)
	}
	database := client.Database("mifolyo_pagerank_contract_" + suffix)
	defer database.Drop(context.Background())
	lock, err := acquirePublicationLock(ctx, database)
	if err != nil {
		t.Fatalf("acquirePublicationLock() error = %v", err)
	}
	if _, err := acquirePublicationLock(ctx, database); err == nil {
		t.Fatal("second acquirePublicationLock() unexpectedly succeeded")
	}
	if err := lock.release(ctx, database); err != nil {
		t.Fatalf("publicationLock.release() error = %v", err)
	}

	nodes := []metadataDocument{
		{ID: "https://a.test/"},
		{ID: "https://b.test/"},
		{ID: "https://c.test/"},
		{ID: "https://d.test/"},
		{ID: "https://e.test/"},
	}
	if _, err := database.Collection(metadataCollection).InsertMany(ctx, nodes); err != nil {
		t.Fatalf("insert metadata: %v", err)
	}
	words := make([]bson.D, 0, len(nodes))
	for _, node := range nodes {
		words = append(words, bson.D{{Key: "url", Value: node.ID}})
	}
	if _, err := database.Collection(wordsCollection).InsertMany(ctx, words); err != nil {
		t.Fatalf("insert words: %v", err)
	}
	if _, err := database.Collection(outlinksCollection).InsertMany(ctx, []outlinkDocument{
		{ID: "https://a.test/", Links: []string{"https://b.test/", "https://b.test/", "https://outside.test/"}},
		{ID: "https://b.test/", Links: []string{"https://c.test/"}},
		{ID: "https://c.test/", Links: []string{"https://a.test/"}},
		{ID: "https://d.test/", Links: []string{"https://outside.test/"}},
		{ID: "https://orphan.test/", Links: []string{"https://a.test/"}},
	}); err != nil {
		t.Fatalf("insert outlinks: %v", err)
	}
	if _, err := database.Collection("backlinks").InsertOne(ctx, bson.D{
		{Key: "_id", Value: "https://a.test/"},
		{Key: "links", Value: bson.A{"https://e.test/"}},
	}); err != nil {
		t.Fatalf("insert contradictory backlinks: %v", err)
	}
	if _, err := database.Collection(pageRankCollection).InsertOne(ctx, bson.D{
		{Key: "_id", Value: "https://stale.test/"},
		{Key: "rank", Value: 1.0},
	}); err != nil {
		t.Fatalf("insert stale PageRank: %v", err)
	}

	input, err := loadGraph(ctx, database)
	if err != nil {
		t.Fatalf("loadGraph() error = %v", err)
	}
	result, err := calculatePageRank(input)
	if err != nil {
		t.Fatalf("calculatePageRank() error = %v", err)
	}
	publication, err := publishPageRank(ctx, client, database, input, result)
	if err != nil {
		t.Fatalf("publishPageRank() error = %v", err)
	}
	if publication.Status != "published" || publication.RunID == "" {
		t.Fatalf("unexpected publication: %#v", publication)
	}

	var published []pageRankDocument
	cursor, err := database.Collection(pageRankCollection).Find(ctx, bson.D{})
	if err != nil {
		t.Fatalf("read published PageRank: %v", err)
	}
	if err := cursor.All(ctx, &published); err != nil {
		t.Fatalf("decode published PageRank: %v", err)
	}
	if len(published) != len(nodes) {
		t.Fatalf("published count = %d, want %d", len(published), len(nodes))
	}
	for _, document := range published {
		index, exists := sortedNodeIndex(input.Nodes, document.ID)
		if !exists {
			t.Errorf("unexpected published URL %q", document.ID)
			continue
		}
		if math.Abs(document.Rank-result.Ranks[index]) > 1e-15 {
			t.Errorf("rank for %q = %.17g, want %.17g", document.ID, document.Rank, result.Ranks[index])
		}
		if document.RunID != publication.RunID ||
			document.GraphSHA256 != input.SHA256 ||
			document.AlgorithmVersion != algorithmVersion ||
			document.CanonicalizationVersion != canonicalizationVersion {
			t.Errorf("incomplete publication metadata: %#v", document)
		}
	}
	if count, err := database.Collection(pageRankCollection).CountDocuments(
		ctx,
		bson.D{{Key: "_id", Value: "https://stale.test/"}},
	); err != nil || count != 0 {
		t.Fatalf("stale PageRank count = %d, error = %v", count, err)
	}

	collectionNames, err := database.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		t.Fatalf("list collections: %v", err)
	}
	for _, name := range collectionNames {
		if strings.HasPrefix(name, "pagerank_stage_") {
			t.Errorf("staging collection remains after publication: %s", name)
		}
	}

	current, runID, err := activePublicationIsCurrent(ctx, database, input, result)
	if err != nil {
		t.Fatalf("activePublicationIsCurrent() error = %v", err)
	}
	if !current || runID != publication.RunID {
		t.Fatalf("active publication current = %t, run ID = %q", current, runID)
	}
	repeated, err := publishPageRank(ctx, client, database, input, result)
	if err != nil {
		t.Fatalf("repeat publishPageRank() error = %v", err)
	}
	if repeated.Status != "already_current" || repeated.RunID != publication.RunID {
		t.Fatalf("unexpected repeat result: %#v", repeated)
	}

	malformed, err := database.Collection(wordsCollection).InsertOne(ctx, bson.D{
		{Key: "url", Value: bson.A{"https://a.test/", "https://b.test/"}},
	})
	if err != nil {
		t.Fatalf("insert malformed searchable URL: %v", err)
	}
	if _, err := loadGraph(ctx, database); err == nil {
		t.Fatal("loadGraph() accepted an array-valued searchable URL")
	}
	if _, err := database.Collection(wordsCollection).DeleteOne(ctx, bson.D{{Key: "_id", Value: malformed.InsertedID}}); err != nil {
		t.Fatalf("remove malformed searchable URL: %v", err)
	}
}
