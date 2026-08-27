package crawler

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawlpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/database"
	"github.com/IonelPopJara/search-engine/services/spider/internal/pages"
	"github.com/IonelPopJara/search-engine/services/spider/internal/robotsguard"
	"github.com/IonelPopJara/search-engine/services/spider/internal/securefetch"
)

func TestHermeticRedditVariantsHonorDisallowAllWithoutPageRequests(t *testing.T) {
	var requestMu sync.Mutex
	requests := make([]string, 0, 2)
	crawler, db := newHermeticRedditTestCrawler(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestMu.Lock()
		requests = append(requests, request.Host+request.URL.RequestURI())
		requestMu.Unlock()
		if request.URL.Path != "/robots.txt" {
			http.Error(writer, "page request must remain blocked by robots", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "text/plain")
		_, _ = writer.Write([]byte("User-agent: *\nDisallow: /\n"))
	}))

	variants := []string{
		"https://www.reddit.com/r/games",
		"https://old.reddit.com/r/games",
		"https://www.reddit.com/r/games.json",
		"https://old.reddit.com/r/games.json",
	}
	for score, variant := range variants {
		if err := db.PushURLWithDepth(variant, float64(score), 0); err != nil {
			t.Fatalf("queue %s: %v", variant, err)
		}
	}
	queued, err := db.ListPendingURLs()
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != len(variants) {
		t.Fatalf("queued Reddit variants = %d, want %d", len(queued), len(variants))
	}
	queuedURLs := make(map[string]struct{}, len(queued))
	for _, candidate := range queued {
		queuedURLs[candidate.CanonicalURL] = struct{}{}
	}
	for _, variant := range variants {
		if _, exists := queuedURLs[variant]; !exists {
			t.Fatalf("Reddit variant was not queued distinctly: %s", variant)
		}
	}
	crawler.Wg.Add(1)
	crawler.Crawl(db)
	crawler.Wg.Wait()

	requestMu.Lock()
	gotRequests := strings.Join(requests, ",")
	requestMu.Unlock()
	wantRequests := "www.reddit.com/robots.txt,old.reddit.com/robots.txt"
	if gotRequests != wantRequests {
		t.Fatalf("network requests = %s, want only %s", gotRequests, wantRequests)
	}
	if crawler.PageAttempts != 2 || len(crawler.Pages) != 0 || len(crawler.Outlinks) != 0 || len(crawler.Images) != 0 {
		t.Fatalf(
			"unexpected crawl output: attempts=%d pages=%d outlinks=%d images=%d",
			crawler.PageAttempts,
			len(crawler.Pages),
			len(crawler.Outlinks),
			len(crawler.Images),
		)
	}
	pending, err := db.ListPendingURLs()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("robots-denied Reddit variants remained pending: %#v", pending)
	}
}

func TestHermeticTestOnlyRobotsOverrideAllowsSelectedGroupPageFetch(t *testing.T) {
	var requestMu sync.Mutex
	requests := make([]string, 0, 1)
	crawler, db := newHermeticRedditTestCrawler(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestMu.Lock()
		requests = append(requests, request.Host+request.URL.RequestURI())
		requestMu.Unlock()
		switch request.URL.Path {
		case "/robots.txt":
			writer.Header().Set("Content-Type", "text/plain")
			_, _ = writer.Write([]byte("User-agent: *\nDisallow: /\n"))
		case "/r/games":
			writer.Header().Set("Content-Type", "text/html")
			_, _ = writer.Write([]byte("<html><body><p>local Reddit fixture</p></body></html>"))
		default:
			http.NotFound(writer, request)
		}
	}))
	crawler.Robots = testOnlyAllowRobotsForGroups(crawler.Robots, "reddit-crawler")

	const target = "https://www.reddit.com/r/games"
	if err := db.PushURLWithDepth(target, 0, 0); err != nil {
		t.Fatal(err)
	}
	crawler.Wg.Add(1)
	crawler.Crawl(db)
	crawler.Wg.Wait()

	requestMu.Lock()
	gotRequests := strings.Join(requests, ",")
	requestMu.Unlock()
	if gotRequests != "www.reddit.com/r/games" {
		t.Fatalf("test-only override requests = %s, want page without robots fetch", gotRequests)
	}
	if crawler.PageAttempts != 1 || len(crawler.Pages) != 1 {
		t.Fatalf("attempts/pages = %d/%d, want 1/1", crawler.PageAttempts, len(crawler.Pages))
	}
	if _, exists := crawler.Pages[target]; !exists {
		t.Fatalf("test-only override did not store %s", target)
	}
}

func TestTestOnlyRobotsOverrideDelegatesNonSelectedGroups(t *testing.T) {
	fallback := &recordingRobotsAuthorizer{}
	authorizer := testOnlyAllowRobotsForGroups(fallback, "reddit-crawler")

	allowed, err := authorizer.Allowed(
		context.Background(),
		crawlpolicy.Decision{Group: crawlpolicy.Group{ID: "reddit-crawler"}},
		allowRequestGate{},
	)
	if err != nil || !allowed || len(fallback.groupIDs) != 0 {
		t.Fatalf("selected test group was not locally allowed: allowed=%t err=%v calls=%v", allowed, err, fallback.groupIDs)
	}
	allowed, err = authorizer.Allowed(
		context.Background(),
		crawlpolicy.Decision{Group: crawlpolicy.Group{ID: "reference-research"}},
		allowRequestGate{},
	)
	if err != nil || allowed || strings.Join(fallback.groupIDs, ",") != "reference-research" {
		t.Fatalf("non-selected group did not delegate: allowed=%t err=%v calls=%v", allowed, err, fallback.groupIDs)
	}
}

type testOnlyGroupRobotsOverride struct {
	fallback RobotsAuthorizer
	groupIDs map[string]struct{}
}

func testOnlyAllowRobotsForGroups(fallback RobotsAuthorizer, groupIDs ...string) RobotsAuthorizer {
	allowedGroups := make(map[string]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		allowedGroups[groupID] = struct{}{}
	}
	return &testOnlyGroupRobotsOverride{fallback: fallback, groupIDs: allowedGroups}
}

func (override *testOnlyGroupRobotsOverride) Allowed(
	ctx context.Context,
	decision crawlpolicy.Decision,
	gate securefetch.RequestGate,
) (bool, error) {
	if _, allowed := override.groupIDs[decision.Group.ID]; allowed {
		return true, nil
	}
	return override.fallback.Allowed(ctx, decision, gate)
}

type recordingRobotsAuthorizer struct {
	groupIDs []string
}

func (authorizer *recordingRobotsAuthorizer) Allowed(
	_ context.Context,
	decision crawlpolicy.Decision,
	_ securefetch.RequestGate,
) (bool, error) {
	authorizer.groupIDs = append(authorizer.groupIDs, decision.Group.ID)
	return false, nil
}

func newHermeticRedditTestCrawler(t *testing.T, handler http.Handler) (*CrawlerConfig, *database.Database) {
	t.Helper()
	serverTLS, roots := redditTestTLSConfig(t)
	server := httptest.NewUnstartedServer(handler)
	server.TLS = serverTLS
	server.StartTLS()
	t.Cleanup(server.Close)

	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, "")
	}
	policy, err := crawlpolicy.Decode(strings.NewReader(`{
		"schema_version": 1,
		"unmatched_action": "deny",
		"groups": [{
			"id": "reddit-crawler",
			"enabled": true,
			"priority": 1,
			"host_rules": [{"host": "reddit.com", "match": "apex_and_subdomains"}],
			"allowed_schemes": ["https"],
			"max_depth": 1,
			"allow_path_prefixes": ["/r/"],
			"deny_path_prefixes": [],
			"min_request_interval": "0s",
			"max_concurrency": 1,
			"max_requests_per_batch": 10,
			"redirects": {"mode": "same_group", "max_hops": 2},
			"robots": {"mode": "enforce", "on_error": "deny", "cache_ttl": "1h"}
		}]
	}`), "MiFolyoBot/1.0")
	if err != nil {
		t.Fatalf("decode Reddit test policy: %v", err)
	}
	fetcher, err := securefetch.New(securefetch.Config{
		Resolver:       staticResolver{},
		Dialer:         redditTLSDialer{address: strings.TrimPrefix(server.URL, "https://")},
		RootCAs:        roots,
		LocalAddresses: []netip.Addr{},
	})
	if err != nil {
		t.Fatalf("create Reddit test fetcher: %v", err)
	}

	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	db := &database.Database{Client: redisClient, Context: context.Background()}
	return &CrawlerConfig{
		Mu:             &sync.Mutex{},
		Wg:             &sync.WaitGroup{},
		Pages:          make(map[string]*pages.Page),
		Outlinks:       make(map[string]*pages.PageNode),
		Backlinks:      make(map[string]*pages.PageNode),
		Images:         make(map[string][]*pages.Image),
		Aliases:        make(map[string]int),
		MaxPages:       10,
		MaxConcurrency: 1,
		Policy:         policy,
		PolicyRuntime:  policy.NewRuntime(),
		Fetcher:        fetcher,
		Robots:         robotsguard.New(policy, fetcher),
	}, db
}

type redditTLSDialer struct {
	address string
}

func (dialer redditTLSDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	connection, err := (&net.Dialer{}).DialContext(ctx, network, dialer.address)
	if err != nil {
		return nil, err
	}
	return &remoteAddressConn{
		Conn: connection,
		remote: &net.TCPAddr{
			IP:   net.ParseIP("93.184.216.34"),
			Port: 443,
		},
	}, nil
}

func redditTestTLSConfig(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate Reddit test key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "hermetic Reddit crawl test"},
		DNSNames:              []string{"reddit.com", "www.reddit.com", "old.reddit.com"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatalf("create Reddit test certificate: %v", err)
	}
	certificate, err := x509.ParseCertificate(certificateDER)
	if err != nil {
		t.Fatalf("parse Reddit test certificate: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	return &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{certificateDER}, PrivateKey: privateKey}},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"http/1.1"},
	}, roots
}

func TestCrawlCountsRobotsPageAndRedirectAgainstGlobalBudget(t *testing.T) {
	var requestMu sync.Mutex
	requests := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestMu.Lock()
		requests = append(requests, request.URL.Path)
		requestMu.Unlock()
		switch request.URL.Path {
		case "/robots.txt":
			writer.Header().Set("Content-Type", "text/plain")
			_, _ = writer.Write([]byte("User-agent: *\nAllow: /\n"))
		case "/start":
			http.Redirect(writer, request, "/final", http.StatusFound)
		case "/final":
			writer.Header().Set("Content-Type", "text/html")
			_, _ = writer.Write([]byte("<html><body>done</body></html>"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	crawler, db := securityTestCrawler(t, server, 3)
	if err := db.PushURLWithDepth("http://example.com/start", 0, 0); err != nil {
		t.Fatal(err)
	}
	crawler.Wg.Add(1)
	crawler.Crawl(db)
	crawler.Wg.Wait()

	requestMu.Lock()
	gotRequests := strings.Join(requests, ",")
	requestMu.Unlock()
	if gotRequests != "/robots.txt,/start,/final" {
		t.Fatalf("requests = %s", gotRequests)
	}
	if crawler.PageAttempts != 3 {
		t.Fatalf("outbound budget used = %d, want 3", crawler.PageAttempts)
	}
	if _, exists := crawler.Pages["http://example.com/final"]; !exists || len(crawler.Pages) != 1 {
		t.Fatalf("final redirected page identity was not stored: %#v", crawler.Pages)
	}
}

func TestCrawlAppliesRobotsToRedirectTargetBeforeRequest(t *testing.T) {
	var requestMu sync.Mutex
	requests := make([]string, 0, 2)
	robotsBody := "User-agent: *\nDisallow: /private\n"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestMu.Lock()
		requests = append(requests, request.URL.Path)
		requestMu.Unlock()
		switch request.URL.Path {
		case "/robots.txt":
			_, _ = writer.Write([]byte(robotsBody))
		case "/start":
			http.Redirect(writer, request, "/private", http.StatusFound)
		case "/private":
			writer.Header().Set("Content-Type", "text/html")
			_, _ = writer.Write([]byte("must not be fetched"))
		}
	}))
	defer server.Close()

	crawler, db := securityTestCrawler(t, server, 3)
	if err := db.PushURLWithDepth("http://example.com/start", 0, 0); err != nil {
		t.Fatal(err)
	}
	crawler.Wg.Add(1)
	crawler.Crawl(db)
	crawler.Wg.Wait()

	requestMu.Lock()
	gotRequests := strings.Join(requests, ",")
	requestMu.Unlock()
	if gotRequests != "/robots.txt,/start" {
		t.Fatalf("robots-disallowed redirect target reached network: %s", gotRequests)
	}
	if len(crawler.Pages) != 0 || crawler.PageAttempts != 2 {
		t.Fatalf("unexpected crawl result: pages=%d attempts=%d", len(crawler.Pages), crawler.PageAttempts)
	}
}

func TestNoWorkWorkerDoesNotConsumeActiveJobRequestBudget(t *testing.T) {
	var requestMu sync.Mutex
	requests := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestMu.Lock()
		requests = append(requests, request.URL.Path)
		requestMu.Unlock()
		switch request.URL.Path {
		case "/robots.txt":
			_, _ = writer.Write([]byte("User-agent: *\nAllow: /\n"))
		case "/page":
			writer.Header().Set("Content-Type", "text/html")
			_, _ = writer.Write([]byte("<html><body>ok</body></html>"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	crawler, db := securityTestCrawler(t, server, 2)
	if err := db.PushURLWithDepth("http://example.com/page", 0, 0); err != nil {
		t.Fatal(err)
	}
	crawler.Wg.Add(2)
	go crawler.Crawl(db)
	go crawler.Crawl(db)
	crawler.Wg.Wait()

	requestMu.Lock()
	gotRequests := strings.Join(requests, ",")
	requestMu.Unlock()
	if gotRequests != "/robots.txt,/page" {
		t.Fatalf("requests = %s, want robots and page", gotRequests)
	}
	if crawler.PageAttempts != 2 || len(crawler.Pages) != 1 {
		t.Fatalf("attempts/pages = %d/%d, want 2/1", crawler.PageAttempts, len(crawler.Pages))
	}
}

func TestLaterGlobalCapacityDenialRequeuesOriginalCandidate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/robots.txt" {
			_, _ = writer.Write([]byte("User-agent: *\nAllow: /\n"))
			return
		}
		writer.Header().Set("Content-Type", "text/html")
		_, _ = writer.Write([]byte("<html><body>must not fit in this batch</body></html>"))
	}))
	defer server.Close()

	crawler, db := securityTestCrawler(t, server, 1)
	if err := db.PushURLWithDepth("http://example.com/page", 7, 0); err != nil {
		t.Fatal(err)
	}
	crawler.Wg.Add(1)
	crawler.Crawl(db)
	crawler.Wg.Wait()

	candidates, err := db.ListPendingURLs()
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Score != 7 || candidates[0].Depth != 0 {
		t.Fatalf("capacity-denied candidate was not preserved: %#v", candidates)
	}
	if crawler.PageAttempts != 1 || len(crawler.Pages) != 0 {
		t.Fatalf("attempts/pages = %d/%d, want 1/0", crawler.PageAttempts, len(crawler.Pages))
	}
}

func TestLaterGroupBatchDenialRequeuesCandidateAndRefundsGlobalCapacity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/robots.txt" {
			_, _ = writer.Write([]byte("User-agent: *\nAllow: /\n"))
			return
		}
		writer.Header().Set("Content-Type", "text/html")
		_, _ = writer.Write([]byte("<html><body>group budget exhausted</body></html>"))
	}))
	defer server.Close()

	crawler, db, _ := securityTestCrawlerWithLimits(t, server, 2, 1)
	if err := db.PushURLWithDepth("http://example.com/page", 4, 0); err != nil {
		t.Fatal(err)
	}
	crawler.Wg.Add(1)
	crawler.Crawl(db)
	crawler.Wg.Wait()

	candidates, err := db.ListPendingURLs()
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Score != 4 || candidates[0].Depth != 0 {
		t.Fatalf("group-capacity-denied candidate was not preserved: %#v", candidates)
	}
	if crawler.PageAttempts != 1 {
		t.Fatalf("unused global reservation was not refunded: attempts=%d, want 1", crawler.PageAttempts)
	}
}

func TestRedirectAliasReprocessesAtNewlyDiscoveredShallowerDepth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/robots.txt":
			_, _ = writer.Write([]byte("User-agent: *\nAllow: /\n"))
		case "/deep":
			http.Redirect(writer, request, "/target", http.StatusFound)
		case "/target":
			writer.Header().Set("Content-Type", "text/html")
			_, _ = writer.Write([]byte(`<html><body><a href="/child">child</a></body></html>`))
		case "/child":
			writer.Header().Set("Content-Type", "text/html")
			_, _ = writer.Write([]byte("<html><body>child</body></html>"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	crawler, db := securityTestCrawler(t, server, 4)
	if err := db.PushURLWithDepth("http://example.com/deep", 0, 1); err != nil {
		t.Fatal(err)
	}
	if err := db.PushURLWithDepth("http://example.com/target", 2, 0); err != nil {
		t.Fatal(err)
	}
	crawler.Wg.Add(1)
	crawler.Crawl(db)
	crawler.Wg.Wait()

	candidates, err := db.ListPendingURLs()
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].CanonicalURL != "http://example.com/child" || candidates[0].Depth != 1 {
		t.Fatalf("shallower redirect alias did not propagate child depth: %#v", candidates)
	}
	if depth := crawler.Aliases["http://example.com/target"]; depth != 0 {
		t.Fatalf("processed target depth = %d, want 0", depth)
	}
	if len(crawler.Pages) != 1 {
		t.Fatalf("idempotent target page count = %d, want 1", len(crawler.Pages))
	}
}

func TestOutlinkQueueLookupFailureIsReportedAsBatchError(t *testing.T) {
	var redisServer *miniredis.Miniredis
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/robots.txt":
			_, _ = writer.Write([]byte("User-agent: *\nAllow: /\n"))
		case "/page":
			writer.Header().Set("Content-Type", "text/html")
			_, _ = writer.Write([]byte(`<html><body><a href="/child">child</a></body></html>`))
			redisServer.Close()
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	crawler, db, backingRedis := securityTestCrawlerWithRedis(t, server, 3)
	redisServer = backingRedis
	if err := db.PushURLWithDepth("http://example.com/page", 0, 0); err != nil {
		t.Fatal(err)
	}
	crawler.Wg.Add(1)
	crawler.Crawl(db)
	crawler.Wg.Wait()

	if crawler.BatchError == nil {
		t.Fatal("outlink queue lookup failure was not propagated to BatchError")
	}
}

func securityTestCrawler(t *testing.T, server *httptest.Server, maxPages int) (*CrawlerConfig, *database.Database) {
	crawler, db, _ := securityTestCrawlerWithRedis(t, server, maxPages)
	return crawler, db
}

func securityTestCrawlerWithRedis(t *testing.T, server *httptest.Server, maxPages int) (*CrawlerConfig, *database.Database, *miniredis.Miniredis) {
	return securityTestCrawlerWithLimits(t, server, maxPages, maxPages)
}

func securityTestCrawlerWithLimits(t *testing.T, server *httptest.Server, maxPages, maxGroupRequests int) (*CrawlerConfig, *database.Database, *miniredis.Miniredis) {
	t.Helper()
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, "")
	}
	policyDocument := fmt.Sprintf(`{
		"schema_version": 1,
		"unmatched_action": "deny",
		"groups": [{
			"id": "test",
			"enabled": true,
			"priority": 1,
			"host_rules": [{"host": "example.com", "match": "exact"}],
			"allowed_schemes": ["http"],
			"max_depth": 1,
			"allow_path_prefixes": [],
			"deny_path_prefixes": [],
			"min_request_interval": "0s",
			"max_concurrency": 1,
			"max_requests_per_batch": %d,
			"redirects": {"mode": "same_host", "max_hops": 2},
			"robots": {"mode": "enforce", "on_error": "deny", "cache_ttl": "1h"}
		}]
	}`, maxGroupRequests)
	policy, err := crawlpolicy.Decode(strings.NewReader(policyDocument), "TestBot/1.0")
	if err != nil {
		t.Fatalf("decode policy: %v", err)
	}
	fetcher, err := securefetch.New(securefetch.Config{
		Resolver:       staticResolver{},
		Dialer:         forwardingDialer{address: strings.TrimPrefix(server.URL, "http://")},
		LocalAddresses: []netip.Addr{},
	})
	if err != nil {
		t.Fatalf("create fetcher: %v", err)
	}

	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	db := &database.Database{Client: redisClient, Context: context.Background()}
	crawler := &CrawlerConfig{
		Mu:             &sync.Mutex{},
		Wg:             &sync.WaitGroup{},
		Pages:          make(map[string]*pages.Page),
		Outlinks:       make(map[string]*pages.PageNode),
		Backlinks:      make(map[string]*pages.PageNode),
		Images:         make(map[string][]*pages.Image),
		Aliases:        make(map[string]int),
		MaxPages:       maxPages,
		MaxConcurrency: 1,
		Policy:         policy,
		PolicyRuntime:  policy.NewRuntime(),
		Fetcher:        fetcher,
		Robots:         robotsguard.New(policy, fetcher),
	}
	return crawler, db, redisServer
}
