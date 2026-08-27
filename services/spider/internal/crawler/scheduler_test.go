package crawler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawlpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/database"
	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

func TestSchedulerUsesGroupPriorityBeforeQueueScore(t *testing.T) {
	policy := schedulerPolicy(t, 1)
	db := schedulerDatabase(t)
	if err := db.PushURLWithDepth("https://a.example.com/page", 9, 0); err != nil {
		t.Fatalf("push high-priority group URL: %v", err)
	}
	if err := db.PushURLWithDepth("https://b.example.com/page", 0, 0); err != nil {
		t.Fatalf("push low-priority group URL: %v", err)
	}
	crawler := schedulerCrawler(policy)

	job, err := crawler.claimNextURL(context.Background(), db)
	if err != nil {
		t.Fatalf("claim next URL: %v", err)
	}
	defer job.releaseUnusedFirstHop()
	if job.decision.Group.ID != "a" || job.candidate.Score != 9 {
		t.Fatalf("scheduled %#v, want group a despite its higher queue score", job)
	}
}

func TestSchedulerUsesGroupIDBeforeQueueScoreWhenPrioritiesTie(t *testing.T) {
	policy := schedulerPolicyWithPriorities(t, 1, 5, 5)
	db := schedulerDatabase(t)
	if err := db.PushURLWithDepth("https://a.example.com/page", 9, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.PushURLWithDepth("https://b.example.com/page", 0, 0); err != nil {
		t.Fatal(err)
	}
	crawler := schedulerCrawler(policy)

	job, err := crawler.claimNextURL(context.Background(), db)
	if err != nil {
		t.Fatalf("claim next URL: %v", err)
	}
	defer job.releaseUnusedFirstHop()
	if job.decision.Group.ID != "a" {
		t.Fatalf("scheduled group %q, want lexicographically first group a", job.decision.Group.ID)
	}
}

func TestSchedulerSkipsTemporarilyFullHigherPriorityGroup(t *testing.T) {
	policy := schedulerPolicy(t, 1)
	db := schedulerDatabase(t)
	if err := db.PushURLWithDepth("https://a.example.com/page", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.PushURLWithDepth("https://b.example.com/page", 0, 0); err != nil {
		t.Fatal(err)
	}
	crawler := schedulerCrawler(policy)
	held, err := crawler.PolicyRuntime.Acquire(context.Background(), "a")
	if err != nil {
		t.Fatalf("hold group a: %v", err)
	}
	defer held()

	job, err := crawler.claimNextURL(context.Background(), db)
	if err != nil {
		t.Fatalf("claim next URL: %v", err)
	}
	defer job.releaseUnusedFirstHop()
	if job.decision.Group.ID != "b" {
		t.Fatalf("scheduled group %q, want ready group b", job.decision.Group.ID)
	}
}

func TestSchedulerLeavesDepthDeniedCandidatePending(t *testing.T) {
	policy := schedulerPolicy(t, 0)
	db := schedulerDatabase(t)
	if err := db.PushURLWithDepth("https://a.example.com/too-deep", 0, 1); err != nil {
		t.Fatal(err)
	}
	crawler := schedulerCrawler(policy)

	_, err := crawler.claimNextURL(context.Background(), db)
	if !errors.Is(err, ErrNoPolicyCapacity) {
		t.Fatalf("error = %v, want policy capacity stop", err)
	}
	candidates, listErr := db.ListPendingURLs()
	if listErr != nil || len(candidates) != 1 {
		t.Fatalf("denied candidate was not preserved: %#v, %v", candidates, listErr)
	}
}

func TestSchedulerRedactsMalformedQueueMemberFromPropagatedError(t *testing.T) {
	rawMember := "malformed-sensitive-queue-member\nforged-log-line"
	const reference = "368f297bb20f716b"
	db := schedulerDatabase(t)
	if err := db.Client.ZAdd(db.Context, utils.CrawlQueueKeyV1, redis.Z{Score: 1, Member: rawMember}).Err(); err != nil {
		t.Fatal(err)
	}

	crawler := schedulerCrawler(schedulerPolicy(t, 1))
	_, err := crawler.claimNextURL(context.Background(), db)
	if !errors.Is(err, ErrPendingURLInvalid) {
		t.Fatalf("scheduler error = %v, want ErrPendingURLInvalid", err)
	}
	message := err.Error()
	if strings.Contains(message, rawMember) || !strings.Contains(message, "ref="+reference) {
		t.Fatalf("scheduler propagated unredacted queue member: %q", message)
	}
}

func TestCrawlRedactsMalformedQueueMemberFromLogAndBatchError(t *testing.T) {
	rawMember := "malformed-sensitive-queue-member\nforged-log-line"
	const reference = "368f297bb20f716b"
	db := schedulerDatabase(t)
	if err := db.Client.ZAdd(db.Context, utils.CrawlQueueKeyV1, redis.Z{Score: 1, Member: rawMember}).Err(); err != nil {
		t.Fatal(err)
	}

	crawler := schedulerCrawler(schedulerPolicy(t, 1))
	crawler.Wg = &sync.WaitGroup{}
	crawler.Wg.Add(1)

	previousWriter := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()
	var captured bytes.Buffer
	log.SetOutput(&captured)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})

	crawler.Crawl(db)
	if crawler.BatchError == nil {
		t.Fatal("malformed queue member error was not propagated to BatchError")
	}
	for source, message := range map[string]string{
		"batch error": crawler.BatchError.Error(),
		"log output":  captured.String(),
	} {
		if strings.Contains(message, rawMember) || !strings.Contains(message, "ref="+reference) {
			t.Fatalf("%s propagated unredacted queue member: %q", source, message)
		}
	}
}

func TestRequestGateSharesGlobalAndGroupBudgets(t *testing.T) {
	policy := schedulerPolicy(t, 1)
	crawler := schedulerCrawler(policy)
	crawler.MaxPages = 2
	decision, err := policy.Match("https://a.example.com/page", 0)
	if err != nil {
		t.Fatal(err)
	}
	globalCapacity, reserved := crawler.reservePageAttempt()
	if !reserved {
		t.Fatal("reserve initial global request")
	}
	groupCapacity, _, err := crawler.PolicyRuntime.TryReserve("a", time.Now())
	if err != nil || groupCapacity == nil {
		t.Fatalf("reserve initial group request: reservation=%v error=%v", groupCapacity != nil, err)
	}
	gate := newCrawlRequestGate(crawler, "a", groupCapacity, globalCapacity)
	defer gate.closeUnused()

	firstRelease, err := gate.Acquire(context.Background(), decision)
	if err != nil {
		t.Fatalf("use initial request: %v", err)
	}
	firstRelease()
	secondRelease, err := gate.Acquire(context.Background(), decision)
	if err != nil {
		t.Fatalf("reserve second request: %v", err)
	}
	secondRelease()
	if _, err := gate.Acquire(context.Background(), decision); !errors.Is(err, ErrGlobalRequestLimit) {
		t.Fatalf("third request error = %v, want global limit", err)
	}
	if crawler.PageAttempts != 2 {
		t.Fatalf("global attempts = %d, want 2", crawler.PageAttempts)
	}
	availability, err := crawler.PolicyRuntime.Ready("a", time.Now())
	if err != nil || availability.RequestsInBatch != 2 {
		t.Fatalf("group requests in batch = %d, %v; want 2", availability.RequestsInBatch, err)
	}
}

func TestRequestGateCloseUnusedRefundsGlobalAndGroupCapacity(t *testing.T) {
	policy := schedulerPolicyWithLimits(t, 1, 1, 2, 1, 1)
	crawler := schedulerCrawler(policy)
	crawler.MaxPages = 1

	globalCapacity, reserved := crawler.reservePageAttempt()
	if !reserved {
		t.Fatal("reserve initial global request")
	}
	groupCapacity, _, err := crawler.PolicyRuntime.TryReserve("a", time.Now())
	if err != nil || groupCapacity == nil {
		t.Fatalf("reserve initial group request: reservation=%v error=%v", groupCapacity != nil, err)
	}

	gate := newCrawlRequestGate(crawler, "a", groupCapacity, globalCapacity)
	gate.closeUnused()
	gate.closeUnused()

	if crawler.PageAttempts != 0 {
		t.Fatalf("global attempts after refund = %d, want 0", crawler.PageAttempts)
	}
	availability, err := crawler.PolicyRuntime.Ready("a", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !availability.Ready || availability.ActiveRequests != 0 || availability.RequestsInBatch != 0 || !availability.NextStart.IsZero() {
		t.Fatalf("group capacity after refund = %#v, want fully ready", availability)
	}
}

func TestFailedCASRefundsGlobalAndGroupCapacity(t *testing.T) {
	policy := schedulerPolicyWithLimits(t, 1, 1, 2, 1, 1)
	crawler := schedulerCrawler(policy)
	crawler.MaxPages = 1
	db := schedulerDatabase(t)
	if err := db.PushURLWithDepth("https://a.example.com/stale", 2, 0); err != nil {
		t.Fatal(err)
	}
	candidates, err := db.ListPendingURLs()
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates = %#v, %v", candidates, err)
	}
	staleCandidate := candidates[0]
	staleCandidate.Score = 1

	globalCapacity, reserved := crawler.reservePageAttempt()
	if !reserved {
		t.Fatal("reserve initial global request")
	}
	groupCapacity, _, err := crawler.PolicyRuntime.TryReserve("a", time.Now())
	if err != nil || groupCapacity == nil {
		t.Fatalf("reserve initial group request: reservation=%v error=%v", groupCapacity != nil, err)
	}
	claimed, err := claimURLWithReservedCapacity(db, staleCandidate, groupCapacity, globalCapacity)
	if err != nil || claimed {
		t.Fatalf("stale claim = %v, %v", claimed, err)
	}

	if crawler.PageAttempts != 0 {
		t.Fatalf("global attempts after stale claim = %d, want 0", crawler.PageAttempts)
	}
	availability, err := crawler.PolicyRuntime.Ready("a", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !availability.Ready || availability.ActiveRequests != 0 || availability.RequestsInBatch != 0 || !availability.NextStart.IsZero() {
		t.Fatalf("group capacity after stale claim = %#v, want fully ready", availability)
	}
	remaining, err := db.ListPendingURLs()
	if err != nil || len(remaining) != 1 {
		t.Fatalf("stale claim removed candidate: %#v, %v", remaining, err)
	}
}

func TestConcurrentSchedulersDoNotClaimPastGroupBatchCapacity(t *testing.T) {
	policy := schedulerPolicyWithLimits(t, 1, 1, 2, 2, 1)
	db := schedulerDatabase(t)
	if err := db.PushURLWithDepth("https://a.example.com/first", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.PushURLWithDepth("https://a.example.com/second", 1, 0); err != nil {
		t.Fatal(err)
	}
	crawler := schedulerCrawler(policy)

	firstJob, err := crawler.claimNextURL(context.Background(), db)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}

	blockedContext, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := crawler.claimNextURL(blockedContext, db); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second claim with provisional exhaustion error = %v, want context deadline", err)
	}

	groupRelease, err := firstJob.firstGroupCapacity.Commit()
	if err != nil {
		t.Fatalf("commit first group capacity: %v", err)
	}
	firstJob.firstGroupCapacity = nil
	firstJob.firstGlobalCapacity.commit()
	firstJob.firstGlobalCapacity = nil
	groupRelease()

	if _, err := crawler.claimNextURL(context.Background(), db); !errors.Is(err, ErrNoPolicyCapacity) {
		t.Fatalf("second claim error = %v, want ErrNoPolicyCapacity", err)
	}

	candidates, err := db.ListPendingURLs()
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].CanonicalURL != "https://a.example.com/second" {
		t.Fatalf("candidate past exhausted group budget was removed: %#v", candidates)
	}
	if crawler.PageAttempts != 1 {
		t.Fatalf("committed global attempts = %d, want 1", crawler.PageAttempts)
	}
	availability, err := crawler.PolicyRuntime.Ready("a", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if availability.Reason != crawlpolicy.AvailabilityBatchLimit || availability.ActiveRequests != 0 || availability.RequestsInBatch != 1 {
		t.Fatalf("committed group capacity = %#v", availability)
	}
}

func schedulerCrawler(policy *crawlpolicy.Policy) *CrawlerConfig {
	return &CrawlerConfig{
		Mu:            &sync.Mutex{},
		Policy:        policy,
		PolicyRuntime: policy.NewRuntime(),
		MaxPages:      100,
	}
}

func schedulerDatabase(t *testing.T) *database.Database {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return &database.Database{Client: client, Context: context.Background()}
}

func schedulerPolicy(t *testing.T, maxDepth int) *crawlpolicy.Policy {
	return schedulerPolicyWithPriorities(t, maxDepth, 1, 2)
}

func schedulerPolicyWithPriorities(t *testing.T, maxDepth, aPriority, bPriority int) *crawlpolicy.Policy {
	return schedulerPolicyWithLimits(t, maxDepth, aPriority, bPriority, 1, 10)
}

func schedulerPolicyWithLimits(t *testing.T, maxDepth, aPriority, bPriority, maxConcurrency, maxRequestsPerBatch int) *crawlpolicy.Policy {
	t.Helper()
	group := func(id, host string, priority int) string {
		return fmt.Sprintf(`{
			"id": %q,
			"enabled": true,
			"priority": %d,
			"host_rules": [{"host": %q, "match": "exact"}],
			"allowed_schemes": ["https"],
			"max_depth": %d,
			"allow_path_prefixes": [],
			"deny_path_prefixes": [],
			"min_request_interval": "0s",
			"max_concurrency": %d,
			"max_requests_per_batch": %d,
			"redirects": {"mode": "same_host", "max_hops": 2},
			"robots": {"mode": "enforce", "on_error": "deny", "cache_ttl": "1h"}
		}`, id, priority, host, maxDepth, maxConcurrency, maxRequestsPerBatch)
	}
	document := fmt.Sprintf(`{
		"schema_version": 1,
		"unmatched_action": "deny",
		"groups": [%s, %s]
	}`, group("a", "a.example.com", aPriority), group("b", "b.example.com", bPriority))
	policy, err := crawlpolicy.Decode(strings.NewReader(document), "TestBot/1.0")
	if err != nil {
		t.Fatalf("decode scheduler policy: %v", err)
	}
	return policy
}
