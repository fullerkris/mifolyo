package robotsguard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawlpolicy"
)

func TestAllowedMatchesRulesUserAgentWildcardsAnchorsAndQueries(t *testing.T) {
	const host = "rules.example.com"
	policy := newTestPolicy(t, testGroupSpec{
		id: "rules", host: host, userAgent: "MiFolyoBot/1.0",
		onError: crawlpolicy.RobotsErrorDeny, ttl: time.Hour,
	})
	body := `User-agent: *
Disallow: /

User-agent: OtherBot
Disallow: /

User-agent: MiFolyoBot
Disallow: /private/
Allow: /private/public/
Disallow: /downloads/*.pdf$
Disallow: /search?blocked=
Disallow: /space%20here
Disallow: /秘密
Disallow: /%70ercent-private
`
	resolver := resolverForHosts(host)
	dialer := &fixtureDialer{handler: func(request *http.Request) (fixtureResponse, error) {
		if request.Host != host || request.URL.RequestURI() != "/robots.txt" {
			return fixtureResponse{}, fmt.Errorf("unexpected robots request %q %q", request.Host, request.URL.RequestURI())
		}
		return fixtureResponse{status: http.StatusOK, body: body}, nil
	}}
	manager := newTestManager(t, policy, resolver, dialer)
	gate := &fixtureGate{}

	tests := []struct {
		name    string
		page    string
		allowed bool
	}{
		{name: "specific user-agent beats wildcard", page: "/open", allowed: true},
		{name: "disallow", page: "/private/file", allowed: false},
		{name: "longer allow", page: "/private/public/file", allowed: true},
		{name: "wildcard and end anchor", page: "/downloads/reports/annual.pdf", allowed: false},
		{name: "end anchor includes query", page: "/downloads/annual.pdf?download=1", allowed: true},
		{name: "query matches", page: "/search?blocked=1", allowed: false},
		{name: "different query", page: "/search?ok=1", allowed: true},
		{name: "canonical escaped path", page: "/space%20here", allowed: false},
		{name: "raw UTF-8 rule", page: "/秘密", allowed: false},
		{name: "encoded unreserved rule", page: "/percent-private", allowed: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := matchPage(t, policy, "http://"+host+test.page)
			allowed, err := manager.Allowed(context.Background(), decision, gate)
			if err != nil {
				t.Fatalf("Allowed failed: %v", err)
			}
			if allowed != test.allowed {
				t.Fatalf("Allowed(%q) = %v, want %v", test.page, allowed, test.allowed)
			}
		})
	}

	dialer.wait(t)
	gate.assertCounts(t, 1, 1)
	requests := dialer.requestSnapshot()
	if len(requests) != 1 || requests[0].userAgent != "MiFolyoBot/1.0" {
		t.Fatalf("robots requests = %#v", requests)
	}
}

func TestRobotsStatusSemantics(t *testing.T) {
	tests := []struct {
		status  int
		body    string
		allowed bool
	}{
		{status: http.StatusNotFound, body: "User-agent: *\nDisallow: /\n", allowed: true},
		{status: http.StatusGone, body: "User-agent: *\nDisallow: /\n", allowed: true},
		{status: http.StatusUnauthorized, body: "User-agent: *\nAllow: /\n", allowed: false},
		{status: http.StatusForbidden, body: "User-agent: *\nAllow: /\n", allowed: false},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("status_%d", test.status), func(t *testing.T) {
			const host = "status.example.com"
			policy := newTestPolicy(t, testGroupSpec{
				id: "status", host: host, userAgent: "StatusBot",
				onError: crawlpolicy.RobotsErrorDeny, ttl: time.Hour,
			})
			dialer := &fixtureDialer{handler: func(*http.Request) (fixtureResponse, error) {
				return fixtureResponse{status: test.status, body: test.body}, nil
			}}
			manager := newTestManager(t, policy, resolverForHosts(host), dialer)
			gate := &fixtureGate{}

			allowed, err := manager.Allowed(context.Background(), matchPage(t, policy, "http://"+host+"/page"), gate)
			if err != nil {
				t.Fatalf("Allowed failed: %v", err)
			}
			if allowed != test.allowed {
				t.Fatalf("Allowed = %v, want %v", allowed, test.allowed)
			}
			dialer.wait(t)
			gate.assertCounts(t, 1, 1)
		})
	}
}

func TestHTMLRobotsResponseUsesParseFailureFallback(t *testing.T) {
	actions := []crawlpolicy.RobotsErrorAction{
		crawlpolicy.RobotsErrorAllow,
		crawlpolicy.RobotsErrorDeny,
	}

	for _, action := range actions {
		t.Run(string(action), func(t *testing.T) {
			const host = "html-robots.example.com"
			policy := newTestPolicy(t, testGroupSpec{
				id: "html-robots", host: host, userAgent: "HTMLRobotsBot",
				onError: action, ttl: time.Hour,
			})
			dialer := &fixtureDialer{handler: func(*http.Request) (fixtureResponse, error) {
				return fixtureResponse{
					status: http.StatusOK,
					header: http.Header{"Content-Type": {"text/html; charset=utf-8"}},
					body:   "Enable JavaScript to continue",
				}, nil
			}}
			manager := newTestManager(t, policy, resolverForHosts(host), dialer)
			gate := &fixtureGate{}

			allowed, err := manager.Allowed(
				context.Background(),
				matchPage(t, policy, "http://"+host+"/page"),
				gate,
			)
			if want := action == crawlpolicy.RobotsErrorAllow; allowed != want {
				t.Fatalf("Allowed = %v, want on_error fallback %v", allowed, want)
			}
			assertRobotsError(t, err, ReasonParseFailed, action)
			dialer.wait(t)
			gate.assertCounts(t, 1, 1)
		})
	}
}

func TestOnErrorFallbacksAreTypedAndCached(t *testing.T) {
	tests := []struct {
		name         string
		reason       Reason
		resolverFail bool
		response     fixtureResponse
	}{
		{
			name: "fetch", reason: ReasonFetchFailed, resolverFail: true,
		},
		{
			name: "server status", reason: ReasonUnexpectedStatus,
			response: fixtureResponse{status: http.StatusServiceUnavailable, body: "temporary failure"},
		},
		{
			name: "parse", reason: ReasonParseFailed,
			response: fixtureResponse{status: http.StatusOK, body: "Disallow: /\n"},
		},
		{
			name: "padded HTML challenge", reason: ReasonParseFailed,
			response: fixtureResponse{status: http.StatusOK, body: strings.Repeat(" ", maxRobotsHTMLSniffBytes+1) + "<body>Client challenge</body>"},
		},
		{
			name: "commented HTML challenge", reason: ReasonParseFailed,
			response: fixtureResponse{status: http.StatusOK, body: "<!-- edge challenge -->\n<head><title>Client challenge</title></head>"},
		},
		{
			name: "XML-prefixed HTML challenge", reason: ReasonParseFailed,
			response: fixtureResponse{status: http.StatusOK, body: "<?xml version=\"1.0\"?>\n<html><body>Client challenge</body></html>"},
		},
		{
			name: "unterminated challenge prolog", reason: ReasonParseFailed,
			response: fixtureResponse{status: http.StatusOK, body: "<!-- Client challenge<html>"},
		},
		{
			name: "invalid UTF-8", reason: ReasonParseFailed,
			response: fixtureResponse{status: http.StatusOK, body: string([]byte{0xff})},
		},
		{
			name: "complexity", reason: ReasonParseFailed,
			response: fixtureResponse{status: http.StatusOK, body: strings.Repeat("User-agent: x\n", maxRobotsUserAgents+1)},
		},
		{
			name: "malformed rule escape", reason: ReasonParseFailed,
			response: fixtureResponse{status: http.StatusOK, body: "User-agent: *\nDisallow: /bad%G0\n"},
		},
	}
	actions := []crawlpolicy.RobotsErrorAction{
		crawlpolicy.RobotsErrorAllow,
		crawlpolicy.RobotsErrorDeny,
	}

	for _, test := range tests {
		for _, action := range actions {
			t.Run(test.name+"_"+string(action), func(t *testing.T) {
				const host = "fallback.example.com"
				policy := newTestPolicy(t, testGroupSpec{
					id: "fallback", host: host, userAgent: "FallbackBot",
					onError: action, ttl: time.Hour,
				})
				resolver := resolverForHosts(host)
				if test.resolverFail {
					resolver.errs[host] = errors.New("simulated DNS failure")
				}
				dialer := &fixtureDialer{handler: func(*http.Request) (fixtureResponse, error) {
					return test.response, nil
				}}
				manager := newTestManager(t, policy, resolver, dialer)
				gate := &fixtureGate{}
				decision := matchPage(t, policy, "http://"+host+"/page")
				wantAllowed := action == crawlpolicy.RobotsErrorAllow

				for call := 0; call < 2; call++ {
					allowed, err := manager.Allowed(context.Background(), decision, gate)
					if allowed != wantAllowed {
						t.Fatalf("call %d Allowed = %v, want %v", call, allowed, wantAllowed)
					}
					assertRobotsError(t, err, test.reason, action)
					if test.reason == ReasonUnexpectedStatus {
						var robotsErr *Error
						errors.As(err, &robotsErr)
						if robotsErr.StatusCode != http.StatusServiceUnavailable {
							t.Fatalf("status = %d", robotsErr.StatusCode)
						}
					}
				}

				dialer.wait(t)
				gate.assertCounts(t, 1, 1)
				if got := resolver.callCount(); got != 1 {
					t.Fatalf("resolver calls = %d, want 1", got)
				}
				wantRequests := 1
				if test.resolverFail {
					wantRequests = 0
				}
				if got := len(dialer.requestSnapshot()); got != wantRequests {
					t.Fatalf("HTTP requests = %d, want %d", got, wantRequests)
				}
			})
		}
	}
}

func TestCacheTTLAndExplicitReset(t *testing.T) {
	const host = "cache.example.com"
	const ttl = 10 * time.Minute
	policy := newTestPolicy(t, testGroupSpec{
		id: "cache", host: host, userAgent: "CacheBot",
		onError: crawlpolicy.RobotsErrorDeny, ttl: ttl,
	})
	var responseMu sync.Mutex
	responseCount := 0
	dialer := &fixtureDialer{handler: func(*http.Request) (fixtureResponse, error) {
		responseMu.Lock()
		responseCount++
		call := responseCount
		responseMu.Unlock()
		body := "User-agent: *\nAllow: /\n"
		if call == 1 {
			body = "User-agent: *\nDisallow: /item\n"
		}
		return fixtureResponse{status: http.StatusOK, body: body}, nil
	}}
	manager := newTestManager(t, policy, resolverForHosts(host), dialer)
	clock := &manualClock{now: time.Unix(1_700_000_000, 0)}
	manager.now = clock.Now
	gate := &fixtureGate{}
	decision := matchPage(t, policy, "http://"+host+"/item")

	assertAllowedResult(t, manager, decision, gate, false)
	assertAllowedResult(t, manager, decision, gate, false)
	gate.assertCounts(t, 1, 1)

	clock.Advance(ttl)
	assertAllowedResult(t, manager, decision, gate, true)
	gate.assertCounts(t, 2, 2)

	manager.ResetCache()
	assertAllowedResult(t, manager, decision, gate, true)
	gate.assertCounts(t, 3, 3)
	dialer.wait(t)
}

func TestConcurrentSingleFlightDoesNotBlockOtherOrigins(t *testing.T) {
	const (
		hostA = "a.concurrent.example.com"
		hostB = "b.concurrent.example.com"
	)
	policy := newTestPolicy(t,
		testGroupSpec{id: "a", host: hostA, userAgent: "AgentA/1.0", onError: crawlpolicy.RobotsErrorDeny, ttl: time.Hour},
		testGroupSpec{id: "b", host: hostB, userAgent: "AgentB/1.0", onError: crawlpolicy.RobotsErrorDeny, ttl: time.Hour},
	)
	startedA := make(chan struct{})
	releaseA := make(chan struct{})
	var signalA sync.Once
	dialer := &fixtureDialer{handler: func(request *http.Request) (fixtureResponse, error) {
		if request.Host == hostA {
			signalA.Do(func() { close(startedA) })
			<-releaseA
		}
		return fixtureResponse{status: http.StatusOK, body: "User-agent: *\nDisallow: /blocked\n"}, nil
	}}
	manager := newTestManager(t, policy, resolverForHosts(hostA, hostB), dialer)
	gate := &fixtureGate{}
	decisionA := matchPage(t, policy, "http://"+hostA+"/blocked")
	decisionB := matchPage(t, policy, "http://"+hostB+"/blocked")

	const callers = 32
	launch := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(callers)
	type result struct {
		allowed bool
		err     error
	}
	results := make(chan result, callers)
	for index := 0; index < callers; index++ {
		go func() {
			ready.Done()
			<-launch
			allowed, err := manager.Allowed(context.Background(), decisionA, gate)
			results <- result{allowed: allowed, err: err}
		}()
	}
	ready.Wait()
	close(launch)
	select {
	case <-startedA:
	case <-time.After(2 * time.Second):
		t.Fatal("origin A fetch did not start")
	}

	otherContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	allowedB, err := manager.Allowed(otherContext, decisionB, gate)
	if err != nil || allowedB {
		t.Fatalf("origin B was blocked or mis-evaluated: allowed=%v err=%v", allowedB, err)
	}
	close(releaseA)

	for index := 0; index < callers; index++ {
		result := <-results
		if result.err != nil || result.allowed {
			t.Fatalf("origin A result = allowed:%v err:%v", result.allowed, result.err)
		}
	}
	dialer.wait(t)
	gate.assertCounts(t, 2, 2)
	requests := dialer.requestSnapshot()
	if len(requests) != 2 {
		t.Fatalf("requests = %#v, want one per origin", requests)
	}
	agents := map[string]string{}
	for _, request := range requests {
		agents[request.host] = request.userAgent
	}
	if agents[hostA] != "AgentA/1.0" || agents[hostB] != "AgentB/1.0" {
		t.Fatalf("origin/user-agent isolation failed: %#v", agents)
	}
	manager.mu.Lock()
	_, cachedA := manager.cache[cacheKey{origin: "http://" + hostA, userAgent: "AgentA/1.0"}]
	_, cachedB := manager.cache[cacheKey{origin: "http://" + hostB, userAgent: "AgentB/1.0"}]
	cacheSize := len(manager.cache)
	manager.mu.Unlock()
	if !cachedA || !cachedB || cacheSize != 2 {
		t.Fatalf("cache was not isolated by canonical origin and user-agent: size=%d A=%v B=%v", cacheSize, cachedA, cachedB)
	}
}

func TestRobotsFetchAndRedirectUseSuppliedGate(t *testing.T) {
	const host = "redirect.example.com"
	policy := newTestPolicy(t, testGroupSpec{
		id: "redirect", host: host, userAgent: "RedirectBot/1.0",
		onError: crawlpolicy.RobotsErrorDeny, ttl: time.Hour,
		allowPrefixes: []string{"/pages/"},
		redirectMode:  crawlpolicy.RedirectSameHost,
		maxHops:       2,
	})
	dialer := &fixtureDialer{handler: func(request *http.Request) (fixtureResponse, error) {
		switch request.URL.Path {
		case "/robots.txt":
			return fixtureResponse{
				status: http.StatusFound,
				header: http.Header{"Location": {"/pages/robot-rules"}},
			}, nil
		case "/pages/robot-rules":
			return fixtureResponse{
				status: http.StatusOK,
				body:   "User-agent: *\nDisallow: /pages/private\n",
			}, nil
		default:
			return fixtureResponse{}, fmt.Errorf("unexpected path %q", request.URL.Path)
		}
	}}
	manager := newTestManager(t, policy, resolverForHosts(host), dialer)
	gate := &fixtureGate{}
	decision := matchPage(t, policy, "http://"+host+"/pages/private?token=page-secret")

	allowed, err := manager.Allowed(context.Background(), decision, gate)
	if err != nil || allowed {
		t.Fatalf("Allowed = %v, %v; want robots disallow", allowed, err)
	}
	dialer.wait(t)
	gate.assertCounts(t, 2, 2)

	requests := dialer.requestSnapshot()
	if len(requests) != 2 || requests[0].uri != "/robots.txt" || requests[1].uri != "/pages/robot-rules" {
		t.Fatalf("robots request chain = %#v", requests)
	}
	decisions := gate.decisionSnapshot()
	if len(decisions) != len(requests) || decisions[0].Path != "/robots.txt" || decisions[1].Path != "/pages/robot-rules" {
		t.Fatalf("gate decisions = %#v", decisions)
	}
	for _, request := range requests {
		if strings.Contains(request.uri, "page-secret") {
			t.Fatalf("page query leaked into robots request: %q", request.uri)
		}
	}
}

func TestRobotsRedirectRequiresPageScopeAndOriginalHost(t *testing.T) {
	const (
		originalHost = "redirect-source.example.com"
		otherHost    = "redirect-target.example.com"
	)
	tests := []struct {
		name     string
		location string
		hosts    []string
	}{
		{
			name:     "page-denied path",
			location: "/private/robot-rules",
			hosts:    []string{originalHost},
		},
		{
			name:     "cross-host target",
			location: "http://" + otherHost + "/pages/robot-rules",
			hosts:    []string{originalHost, otherHost},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := newTestPolicy(t, testGroupSpec{
				id: "redirect-boundary", host: originalHost, hosts: test.hosts, userAgent: "RedirectBoundaryBot/1.0",
				onError: crawlpolicy.RobotsErrorDeny, ttl: time.Hour,
				allowPrefixes: []string{"/pages/"}, redirectMode: crawlpolicy.RedirectSameGroup, maxHops: 2,
			})
			resolver := resolverForHosts(originalHost, otherHost)
			dialer := &fixtureDialer{handler: func(request *http.Request) (fixtureResponse, error) {
				if request.Host != originalHost || request.URL.Path != "/robots.txt" {
					return fixtureResponse{}, fmt.Errorf("denied redirect reached HTTP: %s%s", request.Host, request.URL.Path)
				}
				return fixtureResponse{status: http.StatusFound, header: http.Header{"Location": {test.location}}}, nil
			}}
			manager := newTestManager(t, policy, resolver, dialer)
			gate := &fixtureGate{}

			allowed, err := manager.Allowed(
				context.Background(),
				matchPage(t, policy, "http://"+originalHost+"/pages/item"),
				gate,
			)
			if allowed {
				t.Fatal("denied robots redirect was allowed")
			}
			assertRobotsError(t, err, ReasonFetchFailed, crawlpolicy.RobotsErrorDeny)
			dialer.wait(t)
			gate.assertCounts(t, 1, 1)
			if got := len(dialer.requestSnapshot()); got != 1 {
				t.Fatalf("HTTP requests = %d, want only initial robots request", got)
			}
			if got := resolver.callCount(); got != 1 {
				t.Fatalf("DNS calls = %d, want only original host", got)
			}
		})
	}
}

func TestGateFailureUsesOnErrorWithoutNetworkBypass(t *testing.T) {
	const host = "gate.example.com"
	policy := newTestPolicy(t, testGroupSpec{
		id: "gate", host: host, userAgent: "GateBot",
		onError: crawlpolicy.RobotsErrorAllow, ttl: time.Hour,
	})
	resolver := resolverForHosts(host)
	dialer := &fixtureDialer{handler: func(*http.Request) (fixtureResponse, error) {
		return fixtureResponse{}, errors.New("HTTP request must not occur")
	}}
	manager := newTestManager(t, policy, resolver, dialer)
	gate := &fixtureGate{err: fmt.Errorf("request budget exhausted: %w", crawlpolicy.ErrBatchLimitReached)}

	decision := matchPage(t, policy, "http://"+host+"/page")
	for call := 0; call < 2; call++ {
		allowed, err := manager.Allowed(context.Background(), decision, gate)
		if !allowed {
			t.Fatalf("call %d: on_error=allow did not allow gate failure", call)
		}
		assertRobotsError(t, err, ReasonFetchFailed, crawlpolicy.RobotsErrorAllow)
		if !errors.Is(err, crawlpolicy.ErrBatchLimitReached) {
			t.Fatalf("call %d: capacity error identity was not preserved: %v", call, err)
		}
	}
	gate.assertCounts(t, 2, 0)
	if resolver.callCount() != 0 || len(dialer.requestSnapshot()) != 0 {
		t.Fatalf("network bypassed failed gate: DNS=%d HTTP=%d", resolver.callCount(), len(dialer.requestSnapshot()))
	}
	manager.mu.Lock()
	cacheSize := len(manager.cache)
	manager.mu.Unlock()
	if cacheSize != 0 {
		t.Fatalf("transient gate denial remained cached: size=%d", cacheSize)
	}
}

func TestRobotsBodyLimitUsesFallback(t *testing.T) {
	const host = "large.example.com"
	policy := newTestPolicy(t, testGroupSpec{
		id: "large", host: host, userAgent: "LargeBot",
		onError: crawlpolicy.RobotsErrorDeny, ttl: time.Hour,
	})
	dialer := &fixtureDialer{handler: func(*http.Request) (fixtureResponse, error) {
		return fixtureResponse{
			status:           http.StatusOK,
			body:             strings.Repeat("x", int(MaxRobotsBodyBytes)+1),
			allowClientClose: true,
		}, nil
	}}
	manager := newTestManager(t, policy, resolverForHosts(host), dialer)
	gate := &fixtureGate{}

	allowed, err := manager.Allowed(context.Background(), matchPage(t, policy, "http://"+host+"/page"), gate)
	if allowed {
		t.Fatal("oversized robots body was allowed")
	}
	assertRobotsError(t, err, ReasonFetchFailed, crawlpolicy.RobotsErrorDeny)
	dialer.wait(t)
	gate.assertCounts(t, 1, 1)
}

func TestInvalidInputsFailClosedBeforeFetch(t *testing.T) {
	const host = "invalid.example.com"
	policy := newTestPolicy(t, testGroupSpec{
		id: "invalid", host: host, userAgent: "InvalidBot",
		onError: crawlpolicy.RobotsErrorAllow, ttl: time.Hour,
	})
	resolver := resolverForHosts(host)
	dialer := &fixtureDialer{}
	manager := newTestManager(t, policy, resolver, dialer)
	decision := matchPage(t, policy, "http://"+host+"/page?token=secret")
	gate := &fixtureGate{}

	tests := []struct {
		name     string
		manager  *Manager
		ctx      context.Context
		decision crawlpolicy.Decision
		gate     crawlpolicyGate
		reason   Reason
	}{
		{name: "nil manager dependencies", manager: &Manager{}, ctx: context.Background(), decision: decision, gate: gate, reason: ReasonInvalidArgument},
		{name: "nil context", manager: manager, decision: decision, gate: gate, reason: ReasonInvalidArgument},
		{name: "nil gate", manager: manager, ctx: context.Background(), decision: decision, reason: ReasonInvalidArgument},
		{name: "zero decision", manager: manager, ctx: context.Background(), gate: gate, reason: ReasonInvalidPageDecision},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allowed, err := test.manager.Allowed(test.ctx, test.decision, test.gate)
			if allowed || ReasonOf(err) != test.reason {
				t.Fatalf("Allowed = %v, reason=%q err=%v", allowed, ReasonOf(err), err)
			}
		})
	}

	tampered := decision
	tampered.URL = cloneURL(decision.URL)
	tampered.URL.RawQuery = "token=changed"
	allowed, err := manager.Allowed(context.Background(), tampered, gate)
	if allowed || ReasonOf(err) != ReasonInvalidPageDecision {
		t.Fatalf("tampered decision = %v, %v", allowed, err)
	}
	if resolver.callCount() != 0 || len(dialer.requestSnapshot()) != 0 {
		t.Fatal("invalid input reached the network")
	}
}

func TestFallbackErrorStringDoesNotExposePageQuery(t *testing.T) {
	const host = "redaction.example.com"
	policy := newTestPolicy(t, testGroupSpec{
		id: "redaction", host: host, userAgent: "RedactionBot",
		onError: crawlpolicy.RobotsErrorAllow, ttl: time.Hour,
	})
	resolver := resolverForHosts(host)
	resolver.errs[host] = errors.New("resolver failed for http://redaction.example.com/?token=transport-secret")
	manager := newTestManager(t, policy, resolver, &fixtureDialer{})
	decision := matchPage(t, policy, "http://"+host+"/page?token=page-secret")

	allowed, err := manager.Allowed(context.Background(), decision, &fixtureGate{})
	if !allowed {
		t.Fatal("on_error=allow fallback was denied")
	}
	assertRobotsError(t, err, ReasonFetchFailed, crawlpolicy.RobotsErrorAllow)
	message := err.Error()
	for _, secret := range []string{"page-secret", "transport-secret", "token="} {
		if strings.Contains(message, secret) {
			t.Fatalf("error exposed query data %q: %q", secret, message)
		}
	}
}

// crawlpolicyGate keeps the table type readable while preserving the exact
// RequestGate method set expected by Allowed.
type crawlpolicyGate interface {
	Acquire(context.Context, crawlpolicy.Decision) (func(), error)
}

func assertAllowedResult(t *testing.T, manager *Manager, decision crawlpolicy.Decision, gate *fixtureGate, want bool) {
	t.Helper()
	allowed, err := manager.Allowed(context.Background(), decision, gate)
	if err != nil || allowed != want {
		t.Fatalf("Allowed = %v, %v; want %v, nil", allowed, err, want)
	}
}

func assertRobotsError(t *testing.T, err error, reason Reason, action crawlpolicy.RobotsErrorAction) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected robots error %q", reason)
	}
	if got := ReasonOf(err); got != reason {
		t.Fatalf("reason = %q (%v), want %q", got, err, reason)
	}
	var robotsErr *Error
	if !errors.As(err, &robotsErr) || robotsErr.Code() != string(reason) || robotsErr.Fallback != action {
		t.Fatalf("unexpected typed error: %#v", err)
	}
	if robotsErr.FallbackAllowed() != (action == crawlpolicy.RobotsErrorAllow) {
		t.Fatalf("FallbackAllowed = %v for %q", robotsErr.FallbackAllowed(), action)
	}
}

type manualClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *manualClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *manualClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	clock.mu.Unlock()
}
