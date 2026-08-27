package robotsguard

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawlpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/securefetch"
)

type testGroupSpec struct {
	id            string
	host          string
	hosts         []string
	userAgent     string
	onError       crawlpolicy.RobotsErrorAction
	ttl           time.Duration
	allowPrefixes []string
	redirectMode  crawlpolicy.RedirectMode
	maxHops       int
}

func newTestPolicy(t *testing.T, specs ...testGroupSpec) *crawlpolicy.Policy {
	t.Helper()
	groups := make([]string, 0, len(specs))
	for _, spec := range specs {
		if spec.ttl == 0 {
			spec.ttl = time.Hour
		}
		if spec.onError == "" {
			spec.onError = crawlpolicy.RobotsErrorDeny
		}
		if spec.redirectMode == "" {
			spec.redirectMode = crawlpolicy.RedirectSameHost
		}
		if spec.maxHops == 0 {
			spec.maxHops = 3
		}
		if spec.allowPrefixes == nil {
			spec.allowPrefixes = []string{}
		}
		allowPrefixes, err := json.Marshal(spec.allowPrefixes)
		if err != nil {
			t.Fatalf("marshal allow prefixes: %v", err)
		}
		hosts := spec.hosts
		if len(hosts) == 0 {
			hosts = []string{spec.host}
		}
		hostRules := make([]map[string]string, 0, len(hosts))
		for _, host := range hosts {
			hostRules = append(hostRules, map[string]string{"host": host, "match": "exact"})
		}
		hostRulesJSON, err := json.Marshal(hostRules)
		if err != nil {
			t.Fatalf("marshal host rules: %v", err)
		}
		groups = append(groups, fmt.Sprintf(`{
            "id":%q,
            "enabled":true,
            "priority":10,
            "host_rules":%s,
            "allowed_schemes":["http"],
            "max_depth":4,
            "allow_path_prefixes":%s,
            "deny_path_prefixes":[],
            "min_request_interval":"0s",
            "max_concurrency":100,
            "max_requests_per_batch":1000,
            "user_agent":%q,
            "redirects":{"mode":%q,"max_hops":%d},
            "robots":{"mode":"enforce","on_error":%q,"cache_ttl":%q}
        }`, spec.id, string(hostRulesJSON), string(allowPrefixes), spec.userAgent, spec.redirectMode, spec.maxHops, spec.onError, spec.ttl.String()))
	}
	document := fmt.Sprintf(`{"schema_version":1,"unmatched_action":"deny","groups":[%s]}`, strings.Join(groups, ","))
	policy, err := crawlpolicy.Decode(strings.NewReader(document), "TestFallbackBot/1.0")
	if err != nil {
		t.Fatalf("Decode policy failed: %v\n%s", err, document)
	}
	return policy
}

func matchPage(t *testing.T, policy *crawlpolicy.Policy, rawURL string) crawlpolicy.Decision {
	t.Helper()
	decision, err := policy.Match(rawURL, 0)
	if err != nil {
		t.Fatalf("Match(%q) failed: %v", rawURL, err)
	}
	return decision
}

func newTestManager(t *testing.T, policy *crawlpolicy.Policy, resolver securefetch.Resolver, dialer securefetch.Dialer) *Manager {
	t.Helper()
	clearProxyEnvironment(t)
	fetcher, err := securefetch.New(securefetch.Config{
		Resolver:              resolver,
		Dialer:                dialer,
		LocalAddresses:        []netip.Addr{},
		DNSLookupTimeout:      2 * time.Second,
		DialTimeout:           2 * time.Second,
		TLSHandshakeTimeout:   2 * time.Second,
		ResponseHeaderTimeout: 2 * time.Second,
		TotalTimeout:          5 * time.Second,
	})
	if err != nil {
		t.Fatalf("securefetch.New failed: %v", err)
	}
	return NewManager(policy, fetcher)
}

func clearProxyEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY",
		"http_proxy", "https_proxy", "all_proxy",
	} {
		t.Setenv(name, "")
	}
}

func resolverForHosts(hosts ...string) *fixtureResolver {
	answers := make(map[string][]netip.Addr, len(hosts))
	for _, host := range hosts {
		answers[host] = []netip.Addr{netip.MustParseAddr("93.184.216.34")}
	}
	return &fixtureResolver{answers: answers, errs: make(map[string]error)}
}

type fixtureResolver struct {
	mu      sync.Mutex
	answers map[string][]netip.Addr
	errs    map[string]error
	calls   []string
}

func (resolver *fixtureResolver) LookupNetIP(_ context.Context, network, host string) ([]netip.Addr, error) {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	resolver.calls = append(resolver.calls, host)
	if network != "ip" {
		return nil, fmt.Errorf("unexpected resolver network %q", network)
	}
	if err := resolver.errs[host]; err != nil {
		return nil, err
	}
	return append([]netip.Addr(nil), resolver.answers[host]...), nil
}

func (resolver *fixtureResolver) callCount() int {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	return len(resolver.calls)
}

type fixtureResponse struct {
	status           int
	header           http.Header
	body             string
	allowClientClose bool
}

type requestObservation struct {
	host      string
	uri       string
	userAgent string
}

type fixtureDialer struct {
	mu       sync.Mutex
	handler  func(*http.Request) (fixtureResponse, error)
	requests []requestObservation
	done     []<-chan error
}

func (dialer *fixtureDialer) DialContext(_ context.Context, network, address string) (net.Conn, error) {
	selected, err := netip.ParseAddrPort(address)
	if err != nil {
		return nil, fmt.Errorf("underlying dial received nonnumeric address %q: %w", address, err)
	}
	client, server := net.Pipe()
	done := make(chan error, 1)
	dialer.mu.Lock()
	dialer.done = append(dialer.done, done)
	handler := dialer.handler
	dialer.mu.Unlock()

	go func() {
		defer server.Close()
		request, readErr := http.ReadRequest(bufio.NewReader(server))
		if readErr != nil {
			done <- readErr
			return
		}
		defer request.Body.Close()
		dialer.mu.Lock()
		dialer.requests = append(dialer.requests, requestObservation{
			host: request.Host, uri: request.URL.RequestURI(), userAgent: request.Header.Get("User-Agent"),
		})
		dialer.mu.Unlock()
		if handler == nil {
			done <- errors.New("no scripted HTTP handler")
			return
		}

		script, handlerErr := handler(request)
		if handlerErr != nil {
			done <- handlerErr
			return
		}
		if script.status == 0 {
			script.status = http.StatusOK
		}
		header := script.header.Clone()
		if header == nil {
			header = make(http.Header)
		}
		response := &http.Response{
			Status:        fmt.Sprintf("%d %s", script.status, http.StatusText(script.status)),
			StatusCode:    script.status,
			Proto:         "HTTP/1.1",
			ProtoMajor:    1,
			ProtoMinor:    1,
			Header:        header,
			Body:          io.NopCloser(strings.NewReader(script.body)),
			ContentLength: int64(len(script.body)),
			Close:         true,
			Request:       request,
		}
		writeErr := response.Write(server)
		if script.allowClientClose && (errors.Is(writeErr, io.ErrClosedPipe) || errors.Is(writeErr, net.ErrClosed)) {
			writeErr = nil
		}
		done <- writeErr
	}()

	return &fixtureRemoteConn{
		Conn:   client,
		remote: net.TCPAddrFromAddrPort(selected),
	}, nil
}

func (dialer *fixtureDialer) requestSnapshot() []requestObservation {
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	return append([]requestObservation(nil), dialer.requests...)
}

func (dialer *fixtureDialer) wait(t *testing.T) {
	t.Helper()
	dialer.mu.Lock()
	doneChannels := append([]<-chan error(nil), dialer.done...)
	dialer.mu.Unlock()
	for _, done := range doneChannels {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("scripted HTTP server failed: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for scripted HTTP server")
		}
	}
}

type fixtureRemoteConn struct {
	net.Conn
	remote net.Addr
}

func (connection *fixtureRemoteConn) RemoteAddr() net.Addr {
	return connection.remote
}

type fixtureGate struct {
	mu        sync.Mutex
	err       error
	acquires  int
	releases  int
	active    int
	decisions []crawlpolicy.Decision
}

func (gate *fixtureGate) Acquire(ctx context.Context, decision crawlpolicy.Decision) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	gate.mu.Lock()
	gate.acquires++
	gate.decisions = append(gate.decisions, decision)
	if gate.err != nil {
		err := gate.err
		gate.mu.Unlock()
		return nil, err
	}
	gate.active++
	gate.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			gate.mu.Lock()
			gate.releases++
			gate.active--
			gate.mu.Unlock()
		})
	}, nil
}

func (gate *fixtureGate) assertCounts(t *testing.T, acquires, releases int) {
	t.Helper()
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.acquires != acquires || gate.releases != releases || gate.active != 0 {
		t.Fatalf("gate = acquire:%d release:%d active:%d, want %d/%d/0", gate.acquires, gate.releases, gate.active, acquires, releases)
	}
}

func (gate *fixtureGate) decisionSnapshot() []crawlpolicy.Decision {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return append([]crawlpolicy.Decision(nil), gate.decisions...)
}

func cloneURL(value *url.URL) *url.URL {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}
