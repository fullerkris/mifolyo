package main

import (
	"context"
	"fmt"
	"os"
	"sort"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func main() {
	mongoHost := getEnv("MONGO_HOST", "localhost")
	mongoPassword := getEnv("MONGO_PASSWORD", "")
	mongoUsername := getEnv("MONGO_USERNAME", "")
	mongoDatabase := getEnv("MONGO_DB", "test")

	fmt.Println("Page Rank Service!")

	mongoURI := fmt.Sprintf("mongodb://%s:27017/", mongoHost)
	if mongoUsername != "" {
		mongoURI = fmt.Sprintf("mongodb://%s:%s@%s:27017/", mongoUsername, mongoPassword, mongoHost)
	}

	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		panic(err)
	}

	defer func() {
		if err := client.Disconnect(context.TODO()); err != nil {
			panic(err)
		}
	}()

	err = client.Ping(context.TODO(), nil)
	if err != nil {
		panic(fmt.Sprintf("Could not ping MongoDB: %v", err))
	}

	fmt.Println("Successfully connected to MongoDB!")

	// Access the test database
	db := client.Database(mongoDatabase)

	// Access the outlinks and backlinks collections
	outlinksColl := db.Collection("outlinks")
	backlinksColl := db.Collection("backlinks")

	// Get the count of documents in the outlinks collection
	count, err := outlinksColl.CountDocuments(context.TODO(), bson.D{})
	if err != nil {
		panic(fmt.Sprintf("Could not count documents in outlinks: %v", err))
	}

	backlinks := make(map[string][]string)
	cursorBacklinks, err := backlinksColl.Find(context.TODO(), bson.D{})
	if err != nil {
		panic(fmt.Sprintf("Could not fetch backlinks: %v", err))
	}
	defer cursorBacklinks.Close(context.TODO())
	for cursorBacklinks.Next(context.TODO()) {
		var doc struct {
			ID    string   `bson:"_id"`
			Links []string `bson:"links"`
		}
		if err := cursorBacklinks.Decode(&doc); err != nil {
			panic(fmt.Sprintf("Could not decode backlink document: %v", err))
		}
		backlinks[doc.ID] = doc.Links
	}

	outlinksCount := make(map[string]int)
	cursorOutlinks, err := outlinksColl.Find(context.TODO(), bson.D{})
	if err != nil {
		panic(fmt.Sprintf("Could not fetch outlinks: %v", err))
	}
	defer cursorOutlinks.Close(context.TODO())
	for cursorOutlinks.Next(context.TODO()) {
		var doc struct {
			ID    string   `bson:"_id"`
			Links []string `bson:"links"`
		}
		if err := cursorOutlinks.Decode(&doc); err != nil {
			panic(fmt.Sprintf("Could not decode outlink document: %v", err))
		}
		outlinksCount[doc.ID] = len(doc.Links)
	}

	pageRank := make(map[string]float64)
	for url := range outlinksCount {
		pageRank[url] = 1.0 / float64(count)
	}

	fmt.Printf("Total number of URLs: %d\n", count)

	iterations := 10
	damping := 0.85
	for i := 0; i < iterations; i++ {
		newPageRank := make(map[string]float64)

		for url, _ := range pageRank {
			var newCumulativeRank float64

			backlinksForUrl, exists := backlinks[url]
			if exists {
				for _, backlink := range backlinksForUrl {
					outlinkCount, ok := outlinksCount[backlink]
					if ok {
						backlinkRank, ok := pageRank[backlink]
						if ok {
							newCumulativeRank += backlinkRank / float64(outlinkCount)
						}
					}
				}
			}

			newPageRank[url] = (1-damping)/float64(count) + damping*newCumulativeRank
		}

		pageRank = newPageRank
	}

	var sortedPageRanks []struct {
		URL  string
		Rank float64
	}
	for url, rank := range pageRank {
		sortedPageRanks = append(sortedPageRanks, struct {
			URL  string
			Rank float64
		}{url, rank})
	}
	sort.Slice(sortedPageRanks, func(i, j int) bool {
		return sortedPageRanks[i].Rank > sortedPageRanks[j].Rank
	})

	// Print sorted page ranks
	fmt.Println("Sorted Page Rank values:")
	for _, pageRank := range sortedPageRanks {
		fmt.Printf("Page URL: %s, Page Rank: %f\n", pageRank.URL, pageRank.Rank)
	}

	var bulkOps []mongo.WriteModel
	for _, pageRank := range sortedPageRanks {
		bulkOps = append(bulkOps, mongo.NewUpdateOneModel().
			SetFilter(bson.D{{Key: "_id", Value: pageRank.URL}}).
			SetUpdate(bson.D{
				{Key: "$set", Value: bson.D{
					{Key: "rank", Value: pageRank.Rank},
				}},
			}).
			SetUpsert(true))
	}

	// Execute the batch insert
	if len(bulkOps) > 0 {
		_, err := db.Collection("pagerank").BulkWrite(context.TODO(), bulkOps)
		if err != nil {
			panic(fmt.Sprintf("Could not batch insert page rank values: %v", err))
		}
	}

	fmt.Println("Page rank values saved to the database!")
}
