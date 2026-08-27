package database

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

func TestRedisV1ScriptsIntegration(t *testing.T) {
	address := os.Getenv("SPIDER_REDIS_INTEGRATION_ADDR")
	if address == "" {
		t.Skip("SPIDER_REDIS_INTEGRATION_ADDR is not configured")
	}

	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("connect to integration Redis: %v", err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	db := &Database{
		Client:         client,
		Context:        ctx,
		CrawlQueueKey:  "mifolyo:test:go:queue:" + suffix,
		CrawlURLsKey:   "mifolyo:test:go:urls:" + suffix,
		CrawlDepthsKey: "mifolyo:test:go:depths:" + suffix,
	}
	t.Cleanup(func() {
		_ = client.Del(ctx, db.queueKey(), db.urlsKey(), db.depthsKey()).Err()
	})

	if err := db.PushURLWithDepth("https://example.com/integration", 2, 2); err != nil {
		t.Fatalf("initial push: %v", err)
	}
	if err := db.PushURLWithDepth("https://example.com/integration", 1, 1); err != nil {
		t.Fatalf("shallower replay: %v", err)
	}
	candidates, err := db.ListPendingURLs()
	if err != nil || len(candidates) != 1 || candidates[0].Score != 1 || candidates[0].Depth != 1 {
		t.Fatalf("integration candidates = %#v, %v", candidates, err)
	}
	claimed, err := db.ClaimURL(candidates[0])
	if err != nil || !claimed {
		t.Fatalf("integration claim = %t, %v", claimed, err)
	}

	identity, err := utils.CanonicalizeURLV1("https://example.com/non-finite-integration")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.HSet(ctx, db.urlsKey(), identity.URLID, identity.CanonicalURL).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.HSet(ctx, db.depthsKey(), identity.URLID, "0").Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.ZAdd(ctx, db.queueKey(), redis.Z{Score: math.Inf(1), Member: identity.URLID}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := db.PushURLWithDepth(identity.CanonicalURL, 0, 0); !errors.Is(err, ErrInvalidQueueScore) {
		t.Fatalf("non-finite existing score error = %v", err)
	}
	if _, err := db.ClaimURL(CrawlCandidate{
		URLID: identity.URLID, CanonicalURL: identity.CanonicalURL, Score: 0, Depth: 0,
	}); !errors.Is(err, ErrInvalidQueueScore) {
		t.Fatalf("claim with non-finite stored score error = %v", err)
	}

	deepIdentity, err := utils.CanonicalizeURLV1("https://example.com/deep-integration")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.HSet(ctx, db.urlsKey(), deepIdentity.URLID, deepIdentity.CanonicalURL).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.HSet(ctx, db.depthsKey(), deepIdentity.URLID, "9007199254740992").Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.ZAdd(ctx, db.queueKey(), redis.Z{Score: 1, Member: deepIdentity.URLID}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := db.PushURLWithDepth(deepIdentity.CanonicalURL, 0, 0); !errors.Is(err, ErrInvalidCrawlDepth) {
		t.Fatalf("inexact existing depth error = %v", err)
	}

	wrongType := &Database{
		Client:         client,
		Context:        ctx,
		CrawlQueueKey:  "mifolyo:test:go:wrong-type:" + suffix,
		CrawlURLsKey:   db.urlsKey(),
		CrawlDepthsKey: db.depthsKey(),
	}
	t.Cleanup(func() { _ = client.Del(ctx, wrongType.queueKey()).Err() })
	if err := client.Set(ctx, wrongType.queueKey(), "not-a-zset", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := wrongType.PushURLWithDepth("https://example.com/wrong-type", 0, 0); err == nil {
		t.Fatal("wrong queue key type was accepted")
	}
}
