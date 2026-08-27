package crawler

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
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
	"github.com/IonelPopJara/search-engine/services/spider/internal/renderclient"
	"github.com/IonelPopJara/search-engine/services/spider/internal/renderpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/robotsguard"
	"github.com/IonelPopJara/search-engine/services/spider/internal/securefetch"
)

type brokerTestRobots struct {
	mu        sync.Mutex
	allowed   bool
	err       error
	decisions []crawlpolicy.Decision
}

func (robots *brokerTestRobots) Allowed(_ context.Context, decision crawlpolicy.Decision, _ securefetch.RequestGate) (bool, error) {
	robots.mu.Lock()
	robots.decisions = append(robots.decisions, cloneBrokerDecision(decision))
	robots.mu.Unlock()
	return robots.allowed, robots.err
}

func (robots *brokerTestRobots) callCount() int {
	robots.mu.Lock()
	defer robots.mu.Unlock()
	return len(robots.decisions)
}

type brokerTestResolver struct {
	mu    sync.Mutex
	hosts []string
}

func (resolver *brokerTestResolver) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	resolver.mu.Lock()
	resolver.hosts = append(resolver.hosts, host)
	resolver.mu.Unlock()
	return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
}

func (resolver *brokerTestResolver) callCount() int {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	return len(resolver.hosts)
}

type brokerTestDialer struct {
	address string
}

func (dialer brokerTestDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
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

type brokerRequestLog struct {
	mu       sync.Mutex
	requests []string
}

func (log *brokerRequestLog) append(request *http.Request) {
	log.mu.Lock()
	log.requests = append(log.requests, request.Host+request.URL.RequestURI())
	log.mu.Unlock()
}

func (log *brokerRequestLog) snapshot() []string {
	log.mu.Lock()
	defer log.mu.Unlock()
	return append([]string(nil), log.requests...)
}

type brokerFixture struct {
	crawler  *CrawlerConfig
	gate     *crawlRequestGate
	rule     renderpolicy.Rule
	origin   crawlpolicy.Decision
	robots   *brokerTestRobots
	resolver *brokerTestResolver
	requests *brokerRequestLog
}

func newBrokerFixture(t *testing.T) *brokerFixture {
	t.Helper()
	return newBrokerFixtureWithGroupRequestLimit(t, 100)
}

func newBrokerFixtureWithGroupRequestLimit(t *testing.T, maxGroupRequests int) *brokerFixture {
	t.Helper()
	requests := &brokerRequestLog{}
	server, roots := newBrokerTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.append(request)
		switch request.URL.Path {
		case "/robots.txt":
			writer.Header().Set("Content-Type", "text/plain")
			_, _ = writer.Write([]byte("User-agent: *\nAllow: /\n"))
		case "/page":
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = writer.Write([]byte("<html><body>source</body></html>"))
		case "/assets/app.js":
			writer.Header().Set("Content-Type", "Application/JavaScript; charset=UTF-8")
			_, _ = writer.Write([]byte("const ok = true;"))
		case "/assets/app.css":
			writer.Header().Set("Content-Type", `text/css; charset="utf-8"; version=1`)
			_, _ = writer.Write([]byte("body{}"))
		case "/assets/duplicate.js":
			writer.Header().Add("Content-Type", "application/javascript")
			writer.Header().Add("Content-Type", "text/javascript")
			_, _ = writer.Write([]byte("duplicate"))
		case "/assets/wrong.js":
			writer.Header().Set("Content-Type", "text/css")
			_, _ = writer.Write([]byte("wrong"))
		case "/assets/malformed.js":
			writer.Header().Set("Content-Type", "application/javascript; charset")
			_, _ = writer.Write([]byte("malformed"))
		case "/assets/latin1.js":
			writer.Header().Set("Content-Type", "application/javascript; charset=iso-8859-1")
			_, _ = writer.Write([]byte("latin1"))
		case "/assets/non-utf8.js":
			writer.Header().Set("Content-Type", "application/javascript")
			_, _ = writer.Write([]byte{0xff, 0xfe})
		case "/assets/status.js":
			writer.Header().Set("Content-Type", "application/javascript")
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte("created"))
		case "/assets/four.js", "/assets/four-a.js", "/assets/four-b.js":
			writer.Header().Set("Content-Type", "application/javascript")
			_, _ = writer.Write([]byte("four"))
		case "/assets/three.js":
			writer.Header().Set("Content-Type", "application/javascript")
			_, _ = writer.Write([]byte("tri"))
		case "/assets/two.js":
			writer.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = writer.Write([]byte("ok"))
		case "/assets/redirect.js":
			http.Redirect(writer, request, "https://cdn.example.org/assets/target.js", http.StatusFound)
		case "/assets/target.js":
			writer.Header().Set("Content-Type", "application/javascript")
			_, _ = writer.Write([]byte("target"))
		default:
			http.NotFound(writer, request)
		}
	}))

	resolver := &brokerTestResolver{}
	fetcher := newBrokerTestFetcher(t, server, roots, resolver)
	policy := brokerCrawlPolicyWithRequestLimit(t, maxGroupRequests)
	renderPolicy := brokerRenderPolicy(t)
	robots := &brokerTestRobots{allowed: true}
	origin, err := policy.Match("https://page.example.org/page", 1)
	if err != nil {
		t.Fatal(err)
	}
	rule, err := renderPolicy.Match(origin.Identity.CanonicalURL)
	if err != nil {
		t.Fatal(err)
	}
	crawler := &CrawlerConfig{
		Mu:            &sync.Mutex{},
		MaxPages:      100,
		Policy:        policy,
		PolicyRuntime: policy.NewRuntime(),
		Fetcher:       fetcher,
		Robots:        robots,
		RenderPolicy:  renderPolicy,
	}
	gate := newCrawlRequestGate(crawler, origin.Group.ID, nil, nil)
	t.Cleanup(gate.Close)
	return &brokerFixture{
		crawler:  crawler,
		gate:     gate,
		rule:     rule,
		origin:   origin,
		robots:   robots,
		resolver: resolver,
		requests: requests,
	}
}

func (fixture *brokerFixture) setRenderLimits(t *testing.T, maxRequests int, maxAggregateBytes, maxBodyBytes int64) {
	t.Helper()
	fixture.crawler.RenderPolicy = brokerRenderPolicyWithLimits(t, maxRequests, maxAggregateBytes, maxBodyBytes)
	rule, err := fixture.crawler.RenderPolicy.Match(fixture.origin.Identity.CanonicalURL)
	if err != nil {
		t.Fatal(err)
	}
	fixture.rule = rule
}

func (fixture *brokerFixture) broker(t *testing.T, rule renderpolicy.Rule) *pageResourceBroker {
	t.Helper()
	broker, err := fixture.crawler.newPageResourceBroker(
		fixture.origin.Identity.CanonicalURL,
		rule,
		1,
		fixture.origin,
		fixture.gate,
	)
	if err != nil {
		t.Fatal(err)
	}
	return broker
}

func TestPageResourceBrokerAuthenticatesConstructionBindings(t *testing.T) {
	for _, test := range []struct {
		name  string
		forge func(*brokerFixture, *renderpolicy.Rule, *crawlpolicy.Decision, *int)
	}{
		{
			name: "forged render rule",
			forge: func(_ *brokerFixture, rule *renderpolicy.Rule, _ *crawlpolicy.Decision, _ *int) {
				rule.ID = "forged"
			},
		},
		{
			name: "forged page decision",
			forge: func(_ *brokerFixture, _ *renderpolicy.Rule, decision *crawlpolicy.Decision, _ *int) {
				decision.MatchedHostRule.Host = "forged.example.org"
			},
		},
		{
			name: "forged depth",
			forge: func(fixture *brokerFixture, _ *renderpolicy.Rule, _ *crawlpolicy.Decision, depth *int) {
				*depth = fixture.origin.Group.MaxDepth + 1
			},
		},
		{
			name: "nil render policy",
			forge: func(fixture *brokerFixture, _ *renderpolicy.Rule, _ *crawlpolicy.Decision, _ *int) {
				fixture.crawler.RenderPolicy = nil
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newBrokerFixture(t)
			rule := cloneBrokerRule(fixture.rule)
			decision := cloneBrokerDecision(fixture.origin)
			depth := 1
			test.forge(fixture, &rule, &decision, &depth)

			broker, err := fixture.crawler.newPageResourceBroker(
				fixture.origin.Identity.CanonicalURL,
				rule,
				depth,
				decision,
				fixture.gate,
			)
			if !errors.Is(err, ErrResourceDenied) || broker != nil {
				t.Fatalf("forged broker=%#v error=%v", broker, err)
			}
			if fixture.robots.callCount() != 0 || fixture.resolver.callCount() != 0 || len(fixture.requests.snapshot()) != 0 {
				t.Fatalf("construction reached dispatch: robots=%d DNS=%d requests=%v", fixture.robots.callCount(), fixture.resolver.callCount(), fixture.requests.snapshot())
			}
		})
	}
}

func TestPageResourceBrokerFetchesStrictJavaScriptAndStylesheet(t *testing.T) {
	fixture := newBrokerFixture(t)
	broker := fixture.broker(t, fixture.rule)

	script, err := broker.Fetch(context.Background(), renderclient.ResourceIntent{
		Method: http.MethodGet,
		URL:    "https://cdn.example.org/assets/app.js",
		Type:   renderpolicy.ResourceTypeScript,
	})
	if err != nil {
		t.Fatal(err)
	}
	stylesheet, err := broker.Fetch(context.Background(), renderclient.ResourceIntent{
		Method: http.MethodGet,
		URL:    "https://cdn.example.org/assets/app.css",
		Type:   renderpolicy.ResourceTypeStylesheet,
	})
	if err != nil {
		t.Fatal(err)
	}
	if script.ContentType != "application/javascript" || string(script.Body) != "const ok = true;" {
		t.Fatalf("script resource = %#v", script)
	}
	if stylesheet.ContentType != "text/css" || string(stylesheet.Body) != "body{}" {
		t.Fatalf("stylesheet resource = %#v", stylesheet)
	}
	if broker.successfulRequests != 2 || broker.approvedBytes != int64(len(script.Body)+len(stylesheet.Body)) {
		t.Fatalf("broker accounting requests=%d bytes=%d", broker.successfulRequests, broker.approvedBytes)
	}
}

func TestPageResourceBrokerDeniesBeforeNetworkAtEachAuthorizationBoundary(t *testing.T) {
	for _, test := range []struct {
		name       string
		url        string
		configure  func(*brokerFixture)
		wantRobots int
	}{
		{
			name: "render rule mismatch",
			url:  "https://cdn.example.org/not-allowed.js",
		},
		{
			name: "crawl group mismatch",
			url:  "https://other.example.org/assets/group.js",
		},
		{
			name: "crawl policy deny",
			url:  "https://cdn.example.org/assets/crawl-denied.js",
		},
		{
			name: "robots deny",
			url:  "https://cdn.example.org/assets/app.js",
			configure: func(fixture *brokerFixture) {
				fixture.robots.allowed = false
			},
			wantRobots: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newBrokerFixture(t)
			if test.configure != nil {
				test.configure(fixture)
			}
			broker := fixture.broker(t, fixture.rule)
			resource, err := broker.Fetch(context.Background(), renderclient.ResourceIntent{
				Method: http.MethodGet,
				URL:    test.url,
				Type:   renderpolicy.ResourceTypeScript,
			})
			if !errors.Is(err, ErrResourceDenied) || len(resource.Body) != 0 {
				t.Fatalf("denied resource=%#v error=%v", resource, err)
			}
			if fixture.robots.callCount() != test.wantRobots || fixture.resolver.callCount() != 0 || len(fixture.requests.snapshot()) != 0 {
				t.Fatalf("denial activity robots=%d DNS=%d requests=%v", fixture.robots.callCount(), fixture.resolver.callCount(), fixture.requests.snapshot())
			}
		})
	}
}

func TestPageResourceBrokerRejectsInvalidIntentEnvelopesBeforeNetwork(t *testing.T) {
	for _, test := range []struct {
		name   string
		intent renderclient.ResourceIntent
	}{
		{
			name: "method",
			intent: renderclient.ResourceIntent{
				Method: http.MethodPost,
				URL:    "https://cdn.example.org/assets/app.js",
				Type:   renderpolicy.ResourceTypeScript,
			},
		},
		{
			name: "type",
			intent: renderclient.ResourceIntent{
				Method: http.MethodGet,
				URL:    "https://cdn.example.org/assets/app.js",
				Type:   renderpolicy.ResourceType("image"),
			},
		},
		{
			name: "HTTP",
			intent: renderclient.ResourceIntent{
				Method: http.MethodGet,
				URL:    "http://cdn.example.org/assets/app.js",
				Type:   renderpolicy.ResourceTypeScript,
			},
		},
		{
			name: "non-canonical",
			intent: renderclient.ResourceIntent{
				Method: http.MethodGet,
				URL:    "https://CDN.example.org/assets/app.js",
				Type:   renderpolicy.ResourceTypeScript,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newBrokerFixture(t)
			broker := fixture.broker(t, fixture.rule)
			resource, err := broker.Fetch(context.Background(), test.intent)
			if !errors.Is(err, ErrResourceDenied) || len(resource.Body) != 0 || fixture.robots.callCount() != 0 ||
				fixture.resolver.callCount() != 0 || len(fixture.requests.snapshot()) != 0 {
				t.Fatalf("invalid intent resource=%#v robots=%d DNS=%d requests=%v error=%v", resource, fixture.robots.callCount(), fixture.resolver.callCount(), fixture.requests.snapshot(), err)
			}
		})
	}
}

func TestPageResourceBrokerRejectsRedirectWithoutTargetPolicyGateOrNetwork(t *testing.T) {
	fixture := newBrokerFixture(t)
	broker := fixture.broker(t, fixture.rule)
	resource, err := broker.Fetch(context.Background(), renderclient.ResourceIntent{
		Method: http.MethodGet,
		URL:    "https://cdn.example.org/assets/redirect.js",
		Type:   renderpolicy.ResourceTypeScript,
	})
	if !errors.Is(err, ErrResourceDenied) || securefetch.ReasonOf(err) != securefetch.ReasonRedirectHopLimit || len(resource.Body) != 0 {
		t.Fatalf("redirect resource=%#v error=%v", resource, err)
	}
	if fixture.robots.callCount() != 1 || fixture.resolver.callCount() != 1 || fixture.crawler.PageAttempts != 1 {
		t.Fatalf("redirect activity robots=%d DNS=%d gate=%d", fixture.robots.callCount(), fixture.resolver.callCount(), fixture.crawler.PageAttempts)
	}
	if requests := fixture.requests.snapshot(); len(requests) != 1 || !strings.HasSuffix(requests[0], "/assets/redirect.js") {
		t.Fatalf("redirect requests = %v", requests)
	}
}

func TestPageResourceBrokerRejectsAmbiguousMIMEAndInvalidUTF8(t *testing.T) {
	for _, test := range []struct {
		name string
		path string
	}{
		{name: "duplicate MIME", path: "duplicate.js"},
		{name: "wrong MIME", path: "wrong.js"},
		{name: "malformed MIME", path: "malformed.js"},
		{name: "wrong charset", path: "latin1.js"},
		{name: "non UTF-8 body", path: "non-utf8.js"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newBrokerFixture(t)
			broker := fixture.broker(t, fixture.rule)
			resource, err := broker.Fetch(context.Background(), renderclient.ResourceIntent{
				Method: http.MethodGet,
				URL:    "https://cdn.example.org/assets/" + test.path,
				Type:   renderpolicy.ResourceTypeScript,
			})
			if !errors.Is(err, ErrResourceDenied) || len(resource.Body) != 0 || broker.approvedBytes != 0 || broker.successfulRequests != 0 {
				t.Fatalf("invalid response resource=%#v requests=%d bytes=%d error=%v", resource, broker.successfulRequests, broker.approvedBytes, err)
			}
		})
	}
}

func TestPageResourceBrokerRequiresDirectStatusOK(t *testing.T) {
	fixture := newBrokerFixture(t)
	broker := fixture.broker(t, fixture.rule)
	resource, err := broker.Fetch(context.Background(), scriptIntent("status.js"))
	if !errors.Is(err, ErrResourceDenied) || len(resource.Body) != 0 || broker.successfulRequests != 0 || broker.approvedBytes != 0 {
		t.Fatalf("non-200 resource=%#v requests=%d bytes=%d error=%v", resource, broker.successfulRequests, broker.approvedBytes, err)
	}
}

func TestPageResourceBrokerEnforcesPerResourceAggregateAndCountLimits(t *testing.T) {
	t.Run("per resource", func(t *testing.T) {
		fixture := newBrokerFixture(t)
		fixture.setRenderLimits(t, fixture.rule.Limits.MaxResourceRequests, fixture.rule.Limits.MaxAggregateResourceBytes, 3)
		broker := fixture.broker(t, fixture.rule)
		resource, err := broker.Fetch(context.Background(), scriptIntent("four.js"))
		if !errors.Is(err, ErrResourceDenied) || securefetch.ReasonOf(err) != securefetch.ReasonBodyTooLarge || len(resource.Body) != 0 || broker.approvedBytes != 0 {
			t.Fatalf("per-resource result=%#v bytes=%d error=%v", resource, broker.approvedBytes, err)
		}
	})

	t.Run("aggregate denied bytes are not approved", func(t *testing.T) {
		fixture := newBrokerFixture(t)
		fixture.setRenderLimits(t, fixture.rule.Limits.MaxResourceRequests, 6, 10)
		broker := fixture.broker(t, fixture.rule)
		first, err := broker.Fetch(context.Background(), scriptIntent("four.js"))
		if err != nil || string(first.Body) != "four" {
			t.Fatalf("first resource=%#v error=%v", first, err)
		}
		denied, err := broker.Fetch(context.Background(), scriptIntent("three.js"))
		if !errors.Is(err, ErrResourceDenied) || len(denied.Body) != 0 || broker.approvedBytes != 4 {
			t.Fatalf("aggregate denial=%#v bytes=%d error=%v", denied, broker.approvedBytes, err)
		}
		last, err := broker.Fetch(context.Background(), scriptIntent("two.js"))
		if err != nil || string(last.Body) != "ok" || broker.approvedBytes != 6 || broker.successfulRequests != 2 {
			t.Fatalf("post-denial resource=%#v requests=%d bytes=%d error=%v", last, broker.successfulRequests, broker.approvedBytes, err)
		}
	})

	t.Run("request count", func(t *testing.T) {
		fixture := newBrokerFixture(t)
		fixture.setRenderLimits(t, 1, fixture.rule.Limits.MaxAggregateResourceBytes, fixture.rule.Limits.MaxResourceBodyBytes)
		broker := fixture.broker(t, fixture.rule)
		if _, err := broker.Fetch(context.Background(), scriptIntent("two.js")); err != nil {
			t.Fatal(err)
		}
		before := len(fixture.requests.snapshot())
		resource, err := broker.Fetch(context.Background(), scriptIntent("four.js"))
		if !errors.Is(err, ErrResourceDenied) || len(resource.Body) != 0 || len(fixture.requests.snapshot()) != before {
			t.Fatalf("count-limit resource=%#v requests=%v error=%v", resource, fixture.requests.snapshot(), err)
		}
	})
}

func TestPageResourceBrokerAccountsConcurrentAggregateResultsAtomically(t *testing.T) {
	fixture := newBrokerFixture(t)
	fixture.setRenderLimits(t, 2, 4, 4)
	broker := fixture.broker(t, fixture.rule)

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, path := range []string{"four-a.js", "four-b.js"} {
		go func(path string) {
			<-start
			resource, err := broker.Fetch(context.Background(), scriptIntent(path))
			if err == nil && string(resource.Body) != "four" {
				err = fmt.Errorf("unexpected body %q", resource.Body)
			}
			results <- err
		}(path)
	}
	close(start)
	successes := 0
	denials := 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrResourceDenied):
			denials++
		default:
			t.Fatalf("concurrent fetch error = %v", err)
		}
	}
	if successes != 1 || denials != 1 || broker.successfulRequests != 1 || broker.approvedBytes != 4 {
		t.Fatalf("concurrent accounting successes=%d denials=%d approved=%d/%d", successes, denials, broker.successfulRequests, broker.approvedBytes)
	}
}

type brokerFetchingRenderer struct {
	mu        sync.Mutex
	broker    renderclient.ResourceBroker
	resources []renderclient.Resource
}

type brokerCapacityRenderer struct {
	err error
}

func (renderer *brokerCapacityRenderer) Render(ctx context.Context, job renderclient.Job) (renderclient.Result, error) {
	if job.Broker == nil {
		renderer.err = errors.New("brokered render job is missing its broker")
		return renderclient.Result{}, renderer.err
	}
	_, renderer.err = job.Broker.Fetch(ctx, scriptIntent("app.js"))
	return renderclient.Result{}, renderer.err
}

func (renderer *brokerFetchingRenderer) Render(ctx context.Context, job renderclient.Job) (renderclient.Result, error) {
	if job.Broker == nil || job.Rule.Mode != renderpolicy.ModeBrokered {
		return renderclient.Result{}, errors.New("brokered render job is missing its broker")
	}
	script, err := job.Broker.Fetch(ctx, scriptIntent("app.js"))
	if err != nil {
		return renderclient.Result{}, err
	}
	stylesheet, err := job.Broker.Fetch(ctx, renderclient.ResourceIntent{
		Method: http.MethodGet,
		URL:    "https://cdn.example.org/assets/app.css",
		Type:   renderpolicy.ResourceTypeStylesheet,
	})
	if err != nil {
		return renderclient.Result{}, err
	}
	renderer.mu.Lock()
	renderer.broker = job.Broker
	renderer.resources = []renderclient.Resource{script, stylesheet}
	renderer.mu.Unlock()
	return renderclient.Result{
		HTML:             "<html><body><main>brokered render</main></body></html>",
		DOMNodes:         3,
		ResourceRequests: 2,
		ResourceBytes:    int64(len(script.Body) + len(stylesheet.Body)),
	}, nil
}

func TestCrawlWiresPageBoundBrokerThroughRenderAndClosesGate(t *testing.T) {
	requests := &brokerRequestLog{}
	server, roots := newBrokerTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.append(request)
		switch request.URL.Path {
		case "/robots.txt":
			writer.Header().Set("Content-Type", "text/plain")
			_, _ = writer.Write([]byte("User-agent: *\nAllow: /\n"))
		case "/page":
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = writer.Write([]byte("<html><body>source</body></html>"))
		case "/assets/app.js":
			writer.Header().Set("Content-Type", "application/javascript; charset=UTF-8")
			_, _ = writer.Write([]byte("const app = true;"))
		case "/assets/app.css":
			writer.Header().Set("Content-Type", "text/css; charset=utf-8")
			_, _ = writer.Write([]byte("body{}"))
		case "/assets/after.js":
			writer.Header().Set("Content-Type", "application/javascript")
			_, _ = writer.Write([]byte("must not be fetched"))
		default:
			http.NotFound(writer, request)
		}
	}))
	resolver := &brokerTestResolver{}
	fetcher := newBrokerTestFetcher(t, server, roots, resolver)
	policy := brokerCrawlPolicy(t)
	renderPolicy := brokerRenderPolicy(t)
	renderer := &brokerFetchingRenderer{}

	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	db := &database.Database{Client: redisClient, Context: context.Background()}
	crawler := &CrawlerConfig{
		Mu:                 &sync.Mutex{},
		Wg:                 &sync.WaitGroup{},
		Pages:              make(map[string]*pages.Page),
		Outlinks:           make(map[string]*pages.PageNode),
		Backlinks:          make(map[string]*pages.PageNode),
		Images:             make(map[string][]*pages.Image),
		Aliases:            make(map[string]int),
		MaxPages:           8,
		MaxConcurrency:     1,
		Policy:             policy,
		PolicyRuntime:      policy.NewRuntime(),
		Fetcher:            fetcher,
		Robots:             robotsguard.New(policy, fetcher),
		RenderPolicy:       renderPolicy,
		RenderPolicySHA256: strings.Repeat("a", 64),
		Renderer:           renderer,
	}
	const pageURL = "https://page.example.org/page"
	if err := db.PushURLWithDepth(pageURL, 0, 1); err != nil {
		t.Fatal(err)
	}
	crawler.Wg.Add(1)
	crawler.Crawl(db)
	crawler.Wg.Wait()

	page := crawler.Pages[pageURL]
	if page == nil || !page.Rendered || !strings.Contains(page.HTML, "brokered render") || crawler.PageAttempts != 5 {
		t.Fatalf("crawl page=%#v attempts=%d", page, crawler.PageAttempts)
	}
	renderer.mu.Lock()
	retainedBroker := renderer.broker
	resources := append([]renderclient.Resource(nil), renderer.resources...)
	renderer.mu.Unlock()
	if len(resources) != 2 || resources[0].ContentType != "application/javascript" || resources[1].ContentType != "text/css" {
		t.Fatalf("render resources = %#v", resources)
	}
	pageBroker, ok := retainedBroker.(*pageResourceBroker)
	if !ok || pageBroker.depth != 1 || pageBroker.effectiveURL != pageURL || pageBroker.pageDecision.Identity.CanonicalURL != pageURL {
		t.Fatalf("page broker binding = %#v", retainedBroker)
	}

	before := requests.snapshot()
	resource, err := retainedBroker.Fetch(context.Background(), scriptIntent("after.js"))
	if !errors.Is(err, ErrResourceDenied) || !errors.Is(err, ErrRequestGateClosed) || len(resource.Body) != 0 {
		t.Fatalf("post-render broker resource=%#v error=%v", resource, err)
	}
	if after := requests.snapshot(); len(after) != len(before) {
		t.Fatalf("closed broker made another request: before=%v after=%v", before, after)
	}
}

func TestCrawlRequeuesExactCandidateWhenBrokerResourceHitsRequestCapacity(t *testing.T) {
	for _, test := range []struct {
		name             string
		maxPages         int
		maxGroupRequests int
		wantErr          error
	}{
		{name: "global", maxPages: 2, maxGroupRequests: 100, wantErr: ErrGlobalRequestLimit},
		{name: "group batch", maxPages: 3, maxGroupRequests: 2, wantErr: crawlpolicy.ErrBatchLimitReached},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newBrokerFixtureWithGroupRequestLimit(t, test.maxGroupRequests)
			crawler := fixture.crawler
			crawler.Wg = &sync.WaitGroup{}
			crawler.Pages = make(map[string]*pages.Page)
			crawler.Outlinks = make(map[string]*pages.PageNode)
			crawler.Backlinks = make(map[string]*pages.PageNode)
			crawler.Images = make(map[string][]*pages.Image)
			crawler.Aliases = make(map[string]int)
			crawler.MaxPages = test.maxPages
			crawler.MaxConcurrency = 1
			crawler.Robots = robotsguard.New(crawler.Policy, crawler.Fetcher)
			crawler.RenderPolicySHA256 = strings.Repeat("a", 64)
			renderer := &brokerCapacityRenderer{}
			crawler.Renderer = renderer

			redisServer := miniredis.RunT(t)
			redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
			t.Cleanup(func() { _ = redisClient.Close() })
			db := &database.Database{Client: redisClient, Context: context.Background()}
			const target = "https://page.example.org/page"
			const score = 7.25
			if err := db.PushURLWithDepth(target, score, 1); err != nil {
				t.Fatal(err)
			}
			crawler.Wg.Add(1)
			crawler.Crawl(db)
			crawler.Wg.Wait()

			if !errors.Is(renderer.err, test.wantErr) {
				t.Fatalf("broker resource error = %v, want %v", renderer.err, test.wantErr)
			}
			pending, err := db.ListPendingURLs()
			if err != nil {
				t.Fatal(err)
			}
			if len(pending) != 1 || pending[0].CanonicalURL != target || pending[0].Score != score || pending[0].Depth != 1 {
				t.Fatalf("requeued candidates = %#v", pending)
			}
			if len(crawler.Pages) != 0 || len(crawler.Outlinks) != 0 || len(crawler.Backlinks) != 0 || len(crawler.Images) != 0 || len(crawler.Aliases) != 0 {
				t.Fatalf("capacity failure leaked publication: pages=%d outlinks=%d backlinks=%d images=%d aliases=%d", len(crawler.Pages), len(crawler.Outlinks), len(crawler.Backlinks), len(crawler.Images), len(crawler.Aliases))
			}
			visited, err := db.HasURLBeenVisited(target)
			if err != nil {
				t.Fatal(err)
			}
			if visited {
				t.Fatal("capacity-denied render was marked visited")
			}
			requests := fixture.requests.snapshot()
			if len(requests) != 2 || requests[0] != "page.example.org/robots.txt" || requests[1] != "page.example.org/page" || crawler.PageAttempts != 2 {
				t.Fatalf("requests=%v attempts=%d, want page robots and page only", requests, crawler.PageAttempts)
			}
		})
	}
}

func scriptIntent(path string) renderclient.ResourceIntent {
	return renderclient.ResourceIntent{
		Method: http.MethodGet,
		URL:    "https://cdn.example.org/assets/" + path,
		Type:   renderpolicy.ResourceTypeScript,
	}
}

func brokerRenderPolicy(t *testing.T) *renderpolicy.Policy {
	t.Helper()
	return brokerRenderPolicyWithLimits(t, 8, 1024, 512)
}

func brokerRenderPolicyWithLimits(t *testing.T, maxRequests int, maxAggregateBytes, maxBodyBytes int64) *renderpolicy.Policy {
	t.Helper()
	document := fmt.Sprintf(`{
		"schema_version": 1,
		"default_action": "deny",
		"rules": [{
			"id": "brokered-test",
			"enabled": true,
			"host_rule": {"host": "page.example.org", "match": "exact"},
			"allow_paths": ["/page"],
			"allow_path_prefixes": [],
			"deny_path_prefixes": [],
			"mode": "brokered",
			"failure_action": "reject_page",
			"resource_rules": [{
				"host_rule": {"host": "cdn.example.org", "match": "exact"},
				"allow_paths": [],
				"allow_path_prefixes": ["/assets/"],
				"deny_path_prefixes": [],
				"allowed_types": ["script", "stylesheet"]
			}, {
				"host_rule": {"host": "other.example.org", "match": "exact"},
				"allow_paths": [],
				"allow_path_prefixes": ["/assets/"],
				"deny_path_prefixes": [],
				"allowed_types": ["script"]
			}],
			"network_controls": {
				"allowed_methods": ["GET"],
				"robots_for_resources": true,
				"allow_cookies": false,
				"allow_service_workers": false,
				"allow_websockets": false,
				"allow_webrtc": false,
				"allow_downloads": false,
				"allow_popups": false,
				"allow_secondary_documents": false,
				"allow_javascript_navigation": false
			},
			"limits": {
				"max_render_time_ms": 1000,
				"settle_time_ms": 0,
				"max_resource_requests": %d,
				"max_aggregate_resource_bytes": %d,
				"max_resource_body_bytes": %d,
				"max_rendered_dom_bytes": 1048576,
				"max_dom_nodes": 1000,
				"max_redirect_hops": 0,
				"max_console_bytes": 1024
			}
		}]
	}`, maxRequests, maxAggregateBytes, maxBodyBytes)
	policy, err := renderpolicy.Decode(strings.NewReader(document))
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func brokerCrawlPolicy(t *testing.T) *crawlpolicy.Policy {
	t.Helper()
	return brokerCrawlPolicyWithRequestLimit(t, 100)
}

func brokerCrawlPolicyWithRequestLimit(t *testing.T, maxGroupRequests int) *crawlpolicy.Policy {
	t.Helper()
	document := fmt.Sprintf(`{
		"schema_version": 1,
		"unmatched_action": "deny",
		"groups": [{
			"id": "pages",
			"enabled": true,
			"priority": 1,
			"host_rules": [
				{"host": "page.example.org", "match": "exact"},
				{"host": "cdn.example.org", "match": "exact"}
			],
			"allowed_schemes": ["https"],
			"max_depth": 2,
			"allow_path_prefixes": [],
			"deny_path_prefixes": ["/assets/crawl-denied"],
			"min_request_interval": "0s",
			"max_concurrency": 4,
			"max_requests_per_batch": %d,
			"redirects": {"mode": "same_group", "max_hops": 2},
			"robots": {"mode": "enforce", "on_error": "deny", "cache_ttl": "1h"}
		}, {
			"id": "other",
			"enabled": true,
			"priority": 2,
			"host_rules": [{"host": "other.example.org", "match": "exact"}],
			"allowed_schemes": ["https"],
			"max_depth": 2,
			"allow_path_prefixes": [],
			"deny_path_prefixes": [],
			"min_request_interval": "0s",
			"max_concurrency": 1,
			"max_requests_per_batch": 10,
			"redirects": {"mode": "none", "max_hops": 0},
			"robots": {"mode": "enforce", "on_error": "deny", "cache_ttl": "1h"}
		}]
	}`, maxGroupRequests)
	policy, err := crawlpolicy.Decode(strings.NewReader(document), "BrokerTestBot/1.0")
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func newBrokerTestFetcher(t *testing.T, server *httptest.Server, roots *x509.CertPool, resolver securefetch.Resolver) *securefetch.Fetcher {
	t.Helper()
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, "")
	}
	fetcher, err := securefetch.New(securefetch.Config{
		Resolver:       resolver,
		Dialer:         brokerTestDialer{address: strings.TrimPrefix(server.URL, "https://")},
		RootCAs:        roots,
		LocalAddresses: []netip.Addr{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return fetcher
}

func newBrokerTLSServer(t *testing.T, handler http.Handler) (*httptest.Server, *x509.CertPool) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "hermetic resource broker test"},
		DNSNames: []string{
			"page.example.org",
			"cdn.example.org",
			"other.example.org",
		},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(certificateDER)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{certificateDER}, PrivateKey: privateKey}},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"http/1.1"},
	}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, roots
}
