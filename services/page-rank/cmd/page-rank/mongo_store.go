package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

const (
	metadataCollection     = "metadata"
	outlinksCollection     = "outlinks"
	wordsCollection        = "words"
	pageRankCollection     = "pagerank"
	pageRankLockCollection = "pagerank_locks"
	pageRankIndexName      = "rank_desc"
	insertBatchSize        = 1000
)

type metadataDocument struct {
	ID string `bson:"_id"`
}

type pageRankDocument struct {
	ID                      string    `bson:"_id"`
	Rank                    float64   `bson:"rank"`
	RunID                   string    `bson:"run_id"`
	GraphSHA256             string    `bson:"graph_sha256"`
	AlgorithmVersion        string    `bson:"algorithm_version"`
	CanonicalizationVersion int       `bson:"canonicalization_version"`
	Iterations              int       `bson:"iterations"`
	Residual                float64   `bson:"residual"`
	PublishedAt             time.Time `bson:"published_at"`
}

type publicationResult struct {
	Status string
	RunID  string
}

type publicationLock struct {
	Owner string
}

type activationOutcomeUnknownError struct {
	cause error
}

func (err *activationOutcomeUnknownError) Error() string {
	return "pagerank: activation outcome unknown; publication lock retained for manual reconciliation"
}

func (err *activationOutcomeUnknownError) Unwrap() error {
	return err.cause
}

func publicationLockCollection(database *mongo.Database) *mongo.Collection {
	return database.Collection(
		pageRankLockCollection,
		options.Collection().SetWriteConcern(writeconcern.Majority()),
	)
}

func acquirePublicationLock(ctx context.Context, database *mongo.Database) (publicationLock, error) {
	owner, err := randomHex(16)
	if err != nil {
		return publicationLock{}, wrapOperationalError("pagerank: create publication lock owner", err)
	}
	_, err = publicationLockCollection(database).InsertOne(ctx, bson.D{
		{Key: "_id", Value: "publication"},
		{Key: "owner", Value: owner},
		{Key: "acquired_at", Value: time.Now().UTC()},
	})
	if mongo.IsDuplicateKeyError(err) {
		return publicationLock{}, fmt.Errorf("pagerank: publication is already locked; review and remove a stale lock manually")
	}
	if err != nil {
		return publicationLock{}, wrapOperationalError("pagerank: acquire publication lock", err)
	}
	return publicationLock{Owner: owner}, nil
}

func (lock publicationLock) release(ctx context.Context, database *mongo.Database) error {
	result, err := publicationLockCollection(database).DeleteOne(ctx, bson.D{
		{Key: "_id", Value: "publication"},
		{Key: "owner", Value: lock.Owner},
	})
	if err != nil {
		return wrapOperationalError("pagerank: release publication lock", err)
	}
	if result.DeletedCount != 1 {
		return fmt.Errorf("pagerank: publication lock ownership changed before release")
	}
	return nil
}

func loadGraph(ctx context.Context, database *mongo.Database) (graph, error) {
	metadataCursor, err := database.Collection(metadataCollection).Find(
		ctx,
		bson.D{},
		options.Find().SetProjection(bson.D{{Key: "_id", Value: 1}}),
	)
	if err != nil {
		return graph{}, wrapOperationalError("pagerank: read metadata", err)
	}
	var metadataDocuments []metadataDocument
	if err := metadataCursor.All(ctx, &metadataDocuments); err != nil {
		return graph{}, wrapOperationalError("pagerank: decode metadata", err)
	}

	nodeIDs := make([]string, 0, len(metadataDocuments))
	for _, document := range metadataDocuments {
		nodeIDs = append(nodeIDs, document.ID)
	}

	outlinksCursor, err := database.Collection(outlinksCollection).Find(
		ctx,
		bson.D{},
		options.Find().SetProjection(bson.D{{Key: "_id", Value: 1}, {Key: "links", Value: 1}}),
	)
	if err != nil {
		return graph{}, wrapOperationalError("pagerank: read outlinks", err)
	}
	var outlinkDocuments []outlinkDocument
	if err := outlinksCursor.All(ctx, &outlinkDocuments); err != nil {
		return graph{}, wrapOperationalError("pagerank: decode outlinks", err)
	}

	loadedGraph, err := buildGraph(nodeIDs, outlinkDocuments)
	if err != nil {
		return graph{}, err
	}
	if err := validateSearchableURLs(ctx, database, loadedGraph.Nodes); err != nil {
		return graph{}, err
	}
	return loadedGraph, nil
}

func validateSearchableURLs(ctx context.Context, database *mongo.Database, nodes []string) error {
	nodeSet := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		nodeSet[node] = struct{}{}
	}

	words := database.Collection(wordsCollection)
	totalCount, err := words.CountDocuments(ctx, bson.D{})
	if err != nil {
		return wrapOperationalError("pagerank: count searchable records", err)
	}
	stringURLCount, err := words.CountDocuments(ctx, bson.D{
		{Key: "$expr", Value: bson.D{
			{Key: "$eq", Value: bson.A{
				bson.D{{Key: "$type", Value: "$url"}},
				"string",
			}},
		}},
	})
	if err != nil {
		return wrapOperationalError("pagerank: validate searchable URL types", err)
	}
	if stringURLCount != totalCount {
		return fmt.Errorf("pagerank: every searchable record must contain one string URL")
	}

	var searchableURLs []string
	err = words.Distinct(ctx, "url", bson.D{}).Decode(&searchableURLs)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil
	}
	if err != nil {
		return wrapOperationalError("pagerank: read distinct searchable URLs", err)
	}

	for _, searchableURL := range searchableURLs {
		if err := validateURLIdentity(searchableURL); err != nil {
			return fmt.Errorf("pagerank: invalid searchable URL ref=%s: %w", valueReference(searchableURL), err)
		}
		if _, exists := nodeSet[searchableURL]; !exists {
			return fmt.Errorf("pagerank: searchable URL ref=%s has no metadata node", valueReference(searchableURL))
		}
	}
	return nil
}

func activePublicationIsCurrent(ctx context.Context, database *mongo.Database, input graph, result pageRankResult) (bool, string, error) {
	collectionNames, err := database.ListCollectionNames(
		ctx,
		bson.D{{Key: "name", Value: pageRankCollection}},
	)
	if err != nil {
		return false, "", wrapOperationalError("pagerank: list active collection", err)
	}
	if len(collectionNames) == 0 {
		return false, "", nil
	}

	cursor, err := database.Collection(pageRankCollection).Find(ctx, bson.D{})
	if err != nil {
		return false, "", wrapOperationalError("pagerank: read active publication", err)
	}
	defer cursor.Close(ctx)

	seen := make(map[string]struct{}, len(input.Nodes))
	runID := ""
	var publishedAt time.Time
	for cursor.Next(ctx) {
		var document pageRankDocument
		if err := cursor.Decode(&document); err != nil {
			return false, "", nil
		}
		index, exists := sortedNodeIndex(input.Nodes, document.ID)
		if !exists {
			return false, "", nil
		}
		if _, duplicate := seen[document.ID]; duplicate {
			return false, "", nil
		}
		seen[document.ID] = struct{}{}
		if math.IsNaN(document.Rank) || math.IsInf(document.Rank, 0) ||
			math.IsNaN(document.Residual) || math.IsInf(document.Residual, 0) ||
			document.GraphSHA256 != input.SHA256 ||
			document.AlgorithmVersion != algorithmVersion ||
			document.CanonicalizationVersion != canonicalizationVersion ||
			document.Iterations != result.Iterations ||
			math.Abs(document.Residual-result.Residual) > 1e-15 ||
			document.PublishedAt.IsZero() ||
			math.Abs(document.Rank-result.Ranks[index]) > 1e-15 {
			return false, "", nil
		}
		if runID == "" {
			runID = document.RunID
			publishedAt = document.PublishedAt
		} else if document.RunID != runID {
			return false, "", nil
		} else if !document.PublishedAt.Equal(publishedAt) {
			return false, "", nil
		}
	}
	if err := cursor.Err(); err != nil {
		return false, "", wrapOperationalError("pagerank: iterate active publication", err)
	}
	if len(seen) != len(input.Nodes) || runID == "" {
		return false, "", nil
	}

	indexCursor, err := database.Collection(pageRankCollection).Indexes().List(ctx)
	if err != nil {
		return false, "", wrapOperationalError("pagerank: list active indexes", err)
	}
	defer indexCursor.Close(ctx)
	foundRankIndex := false
	for indexCursor.Next(ctx) {
		var index struct {
			Name string `bson:"name"`
			Key  bson.D `bson:"key"`
		}
		if err := indexCursor.Decode(&index); err != nil {
			return false, "", wrapOperationalError("pagerank: decode active index", err)
		}
		if index.Name == pageRankIndexName && isDescendingRankIndex(index.Key) {
			foundRankIndex = true
		}
	}
	if err := indexCursor.Err(); err != nil {
		return false, "", wrapOperationalError("pagerank: iterate active indexes", err)
	}
	return foundRankIndex, runID, nil
}

func publishPageRank(ctx context.Context, client *mongo.Client, database *mongo.Database, input graph, result pageRankResult) (publicationResult, error) {
	current, currentRunID, err := activePublicationIsCurrent(ctx, database, input, result)
	if err != nil {
		return publicationResult{}, err
	}
	if current {
		return publicationResult{Status: "already_current", RunID: currentRunID}, nil
	}

	randomSuffix, err := randomHex(12)
	if err != nil {
		return publicationResult{}, wrapOperationalError("pagerank: create run ID", err)
	}
	runID := time.Now().UTC().Format("20060102T150405Z") + "_" + randomSuffix
	stageName := "pagerank_stage_" + randomSuffix
	stage := database.Collection(stageName)
	stageActive := true
	defer func() {
		if !stageActive {
			return
		}
		cleanupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = stage.Drop(cleanupContext)
	}()

	publishedAt := time.Now().UTC()
	documents := make([]pageRankDocument, len(input.Nodes))
	for index, node := range input.Nodes {
		documents[index] = pageRankDocument{
			ID:                      node,
			Rank:                    result.Ranks[index],
			RunID:                   runID,
			GraphSHA256:             input.SHA256,
			AlgorithmVersion:        algorithmVersion,
			CanonicalizationVersion: canonicalizationVersion,
			Iterations:              result.Iterations,
			Residual:                result.Residual,
			PublishedAt:             publishedAt,
		}
	}
	for start := 0; start < len(documents); start += insertBatchSize {
		end := start + insertBatchSize
		if end > len(documents) {
			end = len(documents)
		}
		if _, err := stage.InsertMany(ctx, documents[start:end]); err != nil {
			return publicationResult{}, wrapOperationalError("pagerank: write staging collection", err)
		}
	}

	if _, err := stage.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "rank", Value: -1}},
		Options: options.Index().SetName(pageRankIndexName),
	}); err != nil {
		return publicationResult{}, wrapOperationalError("pagerank: create rank index", err)
	}
	count, err := stage.CountDocuments(ctx, bson.D{})
	if err != nil {
		return publicationResult{}, wrapOperationalError("pagerank: count staging collection", err)
	}
	if count != int64(len(input.Nodes)) {
		return publicationResult{}, fmt.Errorf("pagerank: staging count %d does not match node count %d", count, len(input.Nodes))
	}
	reloadedInput, err := loadGraph(ctx, database)
	if err != nil {
		return publicationResult{}, wrapOperationalError("pagerank: revalidate graph before activation", err)
	}
	if reloadedInput.SHA256 != input.SHA256 {
		return publicationResult{}, fmt.Errorf("pagerank: graph changed before activation")
	}

	from := database.Name() + "." + stageName
	to := database.Name() + "." + pageRankCollection
	if err := client.Database("admin").RunCommand(ctx, bson.D{
		{Key: "renameCollection", Value: from},
		{Key: "to", Value: to},
		{Key: "dropTarget", Value: true},
		{Key: "writeConcern", Value: bson.D{
			{Key: "w", Value: "majority"},
			{Key: "j", Value: true},
		}},
	}).Err(); err != nil {
		verifyContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		current, activeRunID, verifyErr := activePublicationIsCurrent(verifyContext, database, input, result)
		if verifyErr == nil && current && activeRunID == runID {
			stageActive = false
			return publicationResult{Status: "published", RunID: runID}, nil
		}
		return publicationResult{}, &activationOutcomeUnknownError{cause: err}
	}
	stageActive = false
	return publicationResult{Status: "published", RunID: runID}, nil
}

func sortedNodeIndex(nodes []string, value string) (int, bool) {
	index := sort.SearchStrings(nodes, value)
	return index, index < len(nodes) && nodes[index] == value
}

func isDescendingRankIndex(key bson.D) bool {
	if len(key) != 1 || key[0].Key != "rank" {
		return false
	}
	switch direction := key[0].Value.(type) {
	case int32:
		return direction == -1
	case int64:
		return direction == -1
	case float64:
		return direction == -1
	default:
		return false
	}
}

func randomHex(byteCount int) (string, error) {
	value := make([]byte, byteCount)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
