package database

import (
	"context"
	"errors"
	"math"
	"net"
	"strings"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

func TestConnectToRedisFailsClosedWithoutExactLocalOptIn(t *testing.T) {
	server := miniredis.RunT(t)
	host, port, err := net.SplitHostPort(server.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("REDIS_USERNAME", "")
	t.Setenv("ALLOW_INSECURE_DATASTORES", "TRUE")
	if err := (&Database{}).ConnectToRedis(host, port, "", "0"); err == nil {
		t.Fatal("non-exact insecure datastore opt-in was accepted")
	}
	t.Setenv("ALLOW_INSECURE_DATASTORES", "true")
	db := &Database{}
	if err := db.ConnectToRedis(host, port, "", "0"); err != nil {
		t.Fatalf("exact local opt-in was rejected: %v", err)
	}
	t.Cleanup(func() { _ = db.Client.Close() })
}

func testDatabase(t *testing.T) (*Database, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
	})
	return &Database{Client: client, Context: context.Background()}, server
}

func TestMalformedQueueMemberErrorsUseStableReference(t *testing.T) {
	rawMember := "malformed-sensitive-queue-member\nforged-log-line"
	const wantReference = "368f297bb20f716b"

	t.Run("snapshot", func(t *testing.T) {
		db, _ := testDatabase(t)
		if err := db.Client.ZAdd(db.Context, utils.CrawlQueueKeyV1, redis.Z{Score: 1, Member: rawMember}).Err(); err != nil {
			t.Fatal(err)
		}

		candidates, err := db.ListPendingURLs()
		if err != nil || len(candidates) != 1 {
			t.Fatalf("ListPendingURLs = %#v, %v", candidates, err)
		}
		candidate := candidates[0]
		if !errors.Is(candidate.ValidationError, ErrInvalidURLID) {
			t.Fatalf("validation error = %v, want ErrInvalidURLID", candidate.ValidationError)
		}
		if candidate.URLID != "" {
			t.Fatalf("malformed queue member propagated as URL ID: %q", candidate.URLID)
		}
		message := candidate.ValidationError.Error()
		if strings.Contains(message, rawMember) || !strings.Contains(message, "ref="+wantReference) {
			t.Fatalf("validation error did not redact member with stable reference: %q", message)
		}
	})

	t.Run("pop", func(t *testing.T) {
		db, _ := testDatabase(t)
		if err := db.Client.ZAdd(db.Context, utils.CrawlQueueKeyV1, redis.Z{Score: 1, Member: rawMember}).Err(); err != nil {
			t.Fatal(err)
		}

		urlID, canonicalURL, _, err := db.PopURL()
		if !errors.Is(err, ErrInvalidURLID) {
			t.Fatalf("PopURL error = %v, want ErrInvalidURLID", err)
		}
		if urlID != "" || canonicalURL != "" {
			t.Fatalf("PopURL propagated malformed member: URL ID=%q canonical URL=%q", urlID, canonicalURL)
		}
		message := err.Error()
		if strings.Contains(message, rawMember) || !strings.Contains(message, "ref="+wantReference) {
			t.Fatalf("PopURL error did not redact member with stable reference: %q", message)
		}
	})

	t.Run("lookup input", func(t *testing.T) {
		db, _ := testDatabase(t)
		_, _, err := db.ExistsInQueue(rawMember)
		if !errors.Is(err, ErrInvalidURLID) {
			t.Fatalf("ExistsInQueue error = %v, want ErrInvalidURLID", err)
		}
		message := err.Error()
		if strings.Contains(message, rawMember) || !strings.Contains(message, "ref="+wantReference) {
			t.Fatalf("ExistsInQueue error did not redact member with stable reference: %q", message)
		}
	})
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

func TestPushURLWithDepthRetainsBestScoreAndShallowestDepth(t *testing.T) {
	db, _ := testDatabase(t)
	identity, err := utils.CanonicalizeURLV1("https://example.com/depth")
	if err != nil {
		t.Fatalf("canonicalize test URL: %v", err)
	}

	for _, input := range []struct {
		score float64
		depth int
	}{{score: 4, depth: 3}, {score: 2, depth: 5}, {score: 8, depth: 1}} {
		if err := db.PushURLWithDepth(identity.CanonicalURL, input.score, input.depth); err != nil {
			t.Fatalf("push score=%v depth=%d: %v", input.score, input.depth, err)
		}
	}

	candidates, err := db.ListPendingURLs()
	if err != nil {
		t.Fatalf("list pending URLs: %v", err)
	}
	if len(candidates) != 1 || candidates[0].ValidationError != nil {
		t.Fatalf("candidates = %#v", candidates)
	}
	if candidates[0].Score != 2 || candidates[0].Depth != 1 {
		t.Fatalf("candidate score/depth = %v/%d, want 2/1", candidates[0].Score, candidates[0].Depth)
	}
}

func TestListPendingURLsRejectsMissingDepthMetadata(t *testing.T) {
	db, _ := testDatabase(t)
	identity, err := utils.CanonicalizeURLV1("https://example.com/legacy")
	if err != nil {
		t.Fatalf("canonicalize test URL: %v", err)
	}
	if err := db.Client.HSet(db.Context, utils.CrawlURLsKeyV1, identity.URLID, identity.CanonicalURL).Err(); err != nil {
		t.Fatalf("seed URL map: %v", err)
	}
	if err := db.Client.ZAdd(db.Context, utils.CrawlQueueKeyV1, redis.Z{Score: 1, Member: identity.URLID}).Err(); err != nil {
		t.Fatalf("seed queue: %v", err)
	}

	candidates, err := db.ListPendingURLs()
	if err != nil || len(candidates) != 1 {
		t.Fatalf("ListPendingURLs = %#v, %v", candidates, err)
	}
	if candidates[0].ValidationError == nil {
		t.Fatalf("legacy candidate = %#v", candidates[0])
	}
}

func TestListPendingURLsRejectsNoncanonicalOrNegativeDepthMetadata(t *testing.T) {
	for _, depthValue := range []string{"-1", "00", "+1", "1.0"} {
		t.Run(depthValue, func(t *testing.T) {
			db, _ := testDatabase(t)
			identity, err := utils.CanonicalizeURLV1("https://example.com/noncanonical-depth")
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Client.HSet(db.Context, utils.CrawlURLsKeyV1, identity.URLID, identity.CanonicalURL).Err(); err != nil {
				t.Fatal(err)
			}
			if err := db.Client.HSet(db.Context, utils.CrawlDepthsKeyV1, identity.URLID, depthValue).Err(); err != nil {
				t.Fatal(err)
			}
			if err := db.Client.ZAdd(db.Context, utils.CrawlQueueKeyV1, redis.Z{Score: 1, Member: identity.URLID}).Err(); err != nil {
				t.Fatal(err)
			}

			candidates, listErr := db.ListPendingURLs()
			if listErr != nil || len(candidates) != 1 {
				t.Fatalf("ListPendingURLs = %#v, %v", candidates, listErr)
			}
			if !errors.Is(candidates[0].ValidationError, ErrInvalidCrawlDepth) {
				t.Fatalf("validation error = %v, want ErrInvalidCrawlDepth", candidates[0].ValidationError)
			}
		})
	}
}

func TestListPendingURLsRejectsOversizedSnapshot(t *testing.T) {
	db, _ := testDatabase(t)
	db.CrawlSnapshotLimit = 1
	for _, rawURL := range []string{"https://example.com/first", "https://example.com/second"} {
		if err := db.PushURLWithDepth(rawURL, 0, 0); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := db.ListPendingURLs(); !errors.Is(err, ErrQueueSnapshotLimit) {
		t.Fatalf("ListPendingURLs error = %v, want ErrQueueSnapshotLimit", err)
	}
}

func TestPushURLWithDepthRejectsCorruptExistingDepthWithoutPartialUpdate(t *testing.T) {
	db, _ := testDatabase(t)
	identity, err := utils.CanonicalizeURLV1("https://example.com/corrupt-depth")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Client.HSet(db.Context, utils.CrawlURLsKeyV1, identity.URLID, identity.CanonicalURL).Err(); err != nil {
		t.Fatal(err)
	}
	if err := db.Client.HSet(db.Context, utils.CrawlDepthsKeyV1, identity.URLID, "00").Err(); err != nil {
		t.Fatal(err)
	}
	if err := db.Client.ZAdd(db.Context, utils.CrawlQueueKeyV1, redis.Z{Score: 5, Member: identity.URLID}).Err(); err != nil {
		t.Fatal(err)
	}

	if err := db.PushURLWithDepth(identity.CanonicalURL, 1, 0); !errors.Is(err, ErrInvalidCrawlDepth) {
		t.Fatalf("PushURLWithDepth error = %v, want ErrInvalidCrawlDepth", err)
	}
	score, err := db.Client.ZScore(db.Context, utils.CrawlQueueKeyV1, identity.URLID).Result()
	if err != nil || score != 5 {
		t.Fatalf("queue score changed after rejected push: score=%v error=%v", score, err)
	}
	depth, err := db.Client.HGet(db.Context, utils.CrawlDepthsKeyV1, identity.URLID).Result()
	if err != nil || depth != "00" {
		t.Fatalf("depth changed after rejected push: depth=%q error=%v", depth, err)
	}
}

func TestClaimURLRequiresInspectedScore(t *testing.T) {
	db, _ := testDatabase(t)
	identity, err := utils.CanonicalizeURLV1("https://example.com/claim")
	if err != nil {
		t.Fatalf("canonicalize test URL: %v", err)
	}
	if err := db.PushURLWithDepth(identity.CanonicalURL, 2, 0); err != nil {
		t.Fatalf("push URL: %v", err)
	}

	candidate := CrawlCandidate{URLID: identity.URLID, CanonicalURL: identity.CanonicalURL, Score: 1, Depth: 0}
	claimed, err := db.ClaimURL(candidate)
	if err != nil || claimed {
		t.Fatalf("stale claim = %v, %v", claimed, err)
	}
	candidate.Score = 2
	claimed, err = db.ClaimURL(candidate)
	if err != nil || !claimed {
		t.Fatalf("matching claim = %v, %v", claimed, err)
	}
	if _, exists, err := db.ExistsInQueue(identity.URLID); err != nil || exists {
		t.Fatalf("claimed URL still pending: exists=%v error=%v", exists, err)
	}
}

func TestRequeueAfterClaimUsesNewMembershipDepth(t *testing.T) {
	db, _ := testDatabase(t)
	identity, err := utils.CanonicalizeURLV1("https://example.com/requeue")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PushURLWithDepth(identity.CanonicalURL, 0, 0); err != nil {
		t.Fatal(err)
	}
	candidates, err := db.ListPendingURLs()
	if err != nil || len(candidates) != 1 {
		t.Fatalf("initial candidates = %#v, %v", candidates, err)
	}
	claimed, err := db.ClaimURL(candidates[0])
	if err != nil || !claimed {
		t.Fatalf("claim = %v, %v", claimed, err)
	}
	if err := db.PushURLWithDepth(identity.CanonicalURL, 5, 2); err != nil {
		t.Fatal(err)
	}
	candidates, err = db.ListPendingURLs()
	if err != nil || len(candidates) != 1 || candidates[0].Depth != 2 {
		t.Fatalf("requeued candidates = %#v, %v", candidates, err)
	}
}

func TestPushURLWithDepthRejectsNegativeDepth(t *testing.T) {
	db, _ := testDatabase(t)
	if err := db.PushURLWithDepth("https://example.com/", 0, -1); !errors.Is(err, ErrInvalidCrawlDepth) {
		t.Fatalf("error = %v, want ErrInvalidCrawlDepth", err)
	}
}

func TestQueueRejectsNonFiniteScoresAndInexactDepths(t *testing.T) {
	db, _ := testDatabase(t)
	identity, err := utils.CanonicalizeURLV1("https://example.com/non-finite")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Client.HSet(db.Context, utils.CrawlURLsKeyV1, identity.URLID, identity.CanonicalURL).Err(); err != nil {
		t.Fatal(err)
	}
	if err := db.Client.HSet(db.Context, utils.CrawlDepthsKeyV1, identity.URLID, "0").Err(); err != nil {
		t.Fatal(err)
	}
	if err := db.Client.ZAdd(db.Context, utils.CrawlQueueKeyV1, redis.Z{Score: math.Inf(1), Member: identity.URLID}).Err(); err != nil {
		t.Fatal(err)
	}

	candidates, err := db.ListPendingURLs()
	if err != nil || len(candidates) != 1 || !errors.Is(candidates[0].ValidationError, ErrInvalidQueueScore) {
		t.Fatalf("non-finite candidates = %#v, %v", candidates, err)
	}
	if _, _, err := db.ExistsInQueue(identity.URLID); !errors.Is(err, ErrInvalidQueueScore) {
		t.Fatalf("ExistsInQueue error = %v, want ErrInvalidQueueScore", err)
	}
	if err := db.PushURLWithDepth(identity.CanonicalURL, 0, 0); !errors.Is(err, ErrInvalidQueueScore) {
		t.Fatalf("PushURLWithDepth error = %v, want ErrInvalidQueueScore", err)
	}
	if _, err := db.ClaimURL(CrawlCandidate{
		URLID: identity.URLID, CanonicalURL: identity.CanonicalURL, Score: math.Inf(1), Depth: 0,
	}); !errors.Is(err, ErrInvalidQueueScore) {
		t.Fatalf("ClaimURL error = %v, want ErrInvalidQueueScore", err)
	}

	tooDeepValue := utils.MaxCrawlDepthV1 + 1
	if uint64(^uint(0)>>1) < tooDeepValue {
		t.Skip("platform int cannot represent a depth above the Redis Lua exact-integer limit")
	}
	tooDeep := int(tooDeepValue)
	if err := db.PushURLWithDepth("https://example.com/too-deep", 0, tooDeep); !errors.Is(err, ErrInvalidCrawlDepth) {
		t.Fatalf("oversized depth error = %v, want ErrInvalidCrawlDepth", err)
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

func TestPopURLRejectsNonFiniteScoreAndMissingDepth(t *testing.T) {
	t.Run("non-finite score", func(t *testing.T) {
		db, _ := testDatabase(t)
		identity, err := utils.CanonicalizeURLV1("https://example.com/pop-non-finite")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Client.ZAdd(db.Context, utils.CrawlQueueKeyV1, redis.Z{Score: math.Inf(-1), Member: identity.URLID}).Err(); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := db.PopURL(); !errors.Is(err, ErrInvalidQueueScore) {
			t.Fatalf("PopURL error = %v, want ErrInvalidQueueScore", err)
		}
	})

	t.Run("missing depth", func(t *testing.T) {
		db, _ := testDatabase(t)
		identity, err := utils.CanonicalizeURLV1("https://example.com/pop-missing-depth")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.PushURL(identity.CanonicalURL, 0); err != nil {
			t.Fatal(err)
		}
		if err := db.Client.HDel(db.Context, utils.CrawlDepthsKeyV1, identity.URLID).Err(); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := db.PopURL(); !errors.Is(err, ErrInvalidQueueEntry) {
			t.Fatalf("PopURL error = %v, want ErrInvalidQueueEntry", err)
		}
	})
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
