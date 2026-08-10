package database

import (
	"context"
	"errors"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

func testDatabase(t *testing.T) (*Database, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
	})
	return &Database{Client: client, Context: context.Background()}, server
}

func TestPushURLStoresV1LookupAndBestScore(t *testing.T) {
	db, _ := testDatabase(t)
	identity, err := utils.CanonicalizeURLV1("http://example.com/path?x=1")
	if err != nil {
		t.Fatalf("canonicalize test URL: %v", err)
	}

	if err := db.PushURL("http://example.com/path?x=1#first", 4); err != nil {
		t.Fatalf("first push: %v", err)
	}
	if err := db.PushURL("HTTP://Example.COM:80/path?x=1#second", 8); err != nil {
		t.Fatalf("higher-score push: %v", err)
	}
	if err := db.PushURL(identity.CanonicalURL, 2); err != nil {
		t.Fatalf("lower-score push: %v", err)
	}

	storedURL, err := db.Client.HGet(db.Context, utils.CrawlURLsKeyV1, identity.URLID).Result()
	if err != nil {
		t.Fatalf("read URL lookup: %v", err)
	}
	if storedURL != identity.CanonicalURL {
		t.Errorf("stored URL = %q, want exact canonical URL %q", storedURL, identity.CanonicalURL)
	}

	score, exists, err := db.ExistsInQueue(identity.URLID)
	if err != nil {
		t.Fatalf("queue lookup: %v", err)
	}
	if !exists {
		t.Fatal("expected URL ID to exist in queue")
	}
	if score != 2 {
		t.Errorf("queue score = %v, want best score 2", score)
	}

	queuedMembers, err := db.Client.ZRange(db.Context, utils.CrawlQueueKeyV1, 0, -1).Result()
	if err != nil {
		t.Fatalf("read queue members: %v", err)
	}
	if len(queuedMembers) != 1 || queuedMembers[0] != identity.URLID {
		t.Fatalf("queue members = %v, want only URL ID %q", queuedMembers, identity.URLID)
	}
}

func TestPopURLReturnsExactCanonicalURL(t *testing.T) {
	db, _ := testDatabase(t)
	identity, err := utils.CanonicalizeURLV1("http://example.com/path/?b=2&a=1")
	if err != nil {
		t.Fatalf("canonicalize test URL: %v", err)
	}
	if err := db.PushURL(identity.CanonicalURL, 3); err != nil {
		t.Fatalf("push URL: %v", err)
	}

	urlID, canonicalURL, score, err := db.PopURL()
	if err != nil {
		t.Fatalf("pop URL: %v", err)
	}
	if urlID != identity.URLID {
		t.Errorf("URL ID = %q, want %q", urlID, identity.URLID)
	}
	if canonicalURL != identity.CanonicalURL {
		t.Errorf("canonical URL = %q, want %q", canonicalURL, identity.CanonicalURL)
	}
	if score != 3 {
		t.Errorf("score = %v, want 3", score)
	}
	if canonicalURL[:len("http://")] != "http://" {
		t.Fatalf("PopURL changed the stored HTTP scheme: %q", canonicalURL)
	}
}

func TestPushURLRejectsStaticCrawlIneligibility(t *testing.T) {
	db, _ := testDatabase(t)
	err := db.PushURL("http://127.0.0.1/path", 0)
	if err == nil {
		t.Fatal("expected static crawl rejection")
	}
	if code := utils.CrawlAdmissionErrorCode(err); code != utils.CrawlRejectionIPLiteral {
		t.Errorf("crawl rejection = %q, want %q (error: %v)", code, utils.CrawlRejectionIPLiteral, err)
	}

	queueSize, err := db.Client.ZCard(db.Context, utils.CrawlQueueKeyV1).Result()
	if err != nil {
		t.Fatalf("read queue size: %v", err)
	}
	if queueSize != 0 {
		t.Errorf("queue size = %d, want 0", queueSize)
	}
}

func TestPopURLHandlesOrphanLookupSafely(t *testing.T) {
	db, _ := testDatabase(t)
	identity, err := utils.CanonicalizeURLV1("https://example.com/orphan")
	if err != nil {
		t.Fatalf("canonicalize test URL: %v", err)
	}
	if err := db.Client.ZAdd(db.Context, utils.CrawlQueueKeyV1, redis.Z{Score: 1, Member: identity.URLID}).Err(); err != nil {
		t.Fatalf("seed orphan queue member: %v", err)
	}

	urlID, canonicalURL, score, err := db.PopURL()
	if !errors.Is(err, ErrOrphanURLLookup) {
		t.Fatalf("error = %v, want ErrOrphanURLLookup", err)
	}
	if urlID != identity.URLID || canonicalURL != "" || score != 1 {
		t.Errorf("got (%q, %q, %v), want (%q, empty, 1)", urlID, canonicalURL, score, identity.URLID)
	}
}

func TestPopURLRejectsMismatchedLookup(t *testing.T) {
	db, _ := testDatabase(t)
	queuedIdentity, err := utils.CanonicalizeURLV1("https://example.com/queued")
	if err != nil {
		t.Fatalf("canonicalize queued URL: %v", err)
	}
	otherIdentity, err := utils.CanonicalizeURLV1("https://example.com/other")
	if err != nil {
		t.Fatalf("canonicalize lookup URL: %v", err)
	}
	if err := db.Client.HSet(db.Context, utils.CrawlURLsKeyV1, queuedIdentity.URLID, otherIdentity.CanonicalURL).Err(); err != nil {
		t.Fatalf("seed mismatched lookup: %v", err)
	}
	if err := db.Client.ZAdd(db.Context, utils.CrawlQueueKeyV1, redis.Z{Member: queuedIdentity.URLID}).Err(); err != nil {
		t.Fatalf("seed queue member: %v", err)
	}

	_, canonicalURL, _, err := db.PopURL()
	if !errors.Is(err, ErrInvalidQueueEntry) {
		t.Fatalf("error = %v, want ErrInvalidQueueEntry", err)
	}
	if canonicalURL != "" {
		t.Fatalf("corrupt URL lookup must not be returned for fetching: %q", canonicalURL)
	}
}

func TestExistsInQueueRequiresURLID(t *testing.T) {
	db, _ := testDatabase(t)
	_, _, err := db.ExistsInQueue("https://example.com/")
	if !errors.Is(err, ErrInvalidURLID) {
		t.Fatalf("error = %v, want ErrInvalidURLID", err)
	}
}

func TestPushURLDoesNotOverwriteConflictingLookup(t *testing.T) {
	db, _ := testDatabase(t)
	identity, err := utils.CanonicalizeURLV1("https://example.com/collision")
	if err != nil {
		t.Fatalf("canonicalize test URL: %v", err)
	}
	if err := db.Client.HSet(db.Context, utils.CrawlURLsKeyV1, identity.URLID, "https://example.com/different").Err(); err != nil {
		t.Fatalf("seed conflicting lookup: %v", err)
	}

	err = db.PushURL(identity.CanonicalURL, 1)
	if !errors.Is(err, ErrURLIDCollision) {
		t.Fatalf("error = %v, want ErrURLIDCollision", err)
	}
	stored, err := db.Client.HGet(db.Context, utils.CrawlURLsKeyV1, identity.URLID).Result()
	if err != nil {
		t.Fatalf("read conflicting lookup: %v", err)
	}
	if stored != "https://example.com/different" {
		t.Errorf("conflicting lookup was overwritten with %q", stored)
	}
}
