package crawler

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawlpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/securefetch"
)

func TestGetPageData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Host != "example.com" || request.URL.Path != "/page" {
			http.Error(w, "unexpected request target", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "Text/HTML; charset=UTF-8")
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	}))
	defer server.Close()

	fetcher, policy := testPageFetcher(t, server)
	data, err := getPageData(
		context.Background(),
		fetcher,
		"http://example.com/page",
		func(rawURL string) (crawlpolicy.Decision, error) { return policy.Match(rawURL, 0) },
		allowRequestGate{},
		nil,
	)
	if err != nil {
		t.Fatalf("getPageData failed: %v", err)
	}
	if data.HTML != "<html><body>ok</body></html>" || data.EffectiveURL != "http://example.com/page" || data.StatusCode != http.StatusOK {
		t.Fatalf("unexpected page data: %#v", data)
	}
}

func TestValidatePageHTMLResponseRejectsAmbiguousMIMEAndInvalidUTF8(t *testing.T) {
	validBody := []byte("<html></html>")
	for _, test := range []struct {
		name        string
		contentType string
		values      []string
		body        []byte
	}{
		{name: "missing MIME", contentType: "", values: nil, body: validBody},
		{name: "duplicate MIME", contentType: "text/html", values: []string{"text/html", "text/html"}, body: validBody},
		{name: "exposed MIME mismatch", contentType: "text/html", values: []string{"Text/HTML"}, body: validBody},
		{name: "padded MIME", contentType: " text/html", values: []string{" text/html"}, body: validBody},
		{name: "malformed MIME", contentType: "text/html; charset", values: []string{"text/html; charset"}, body: validBody},
		{name: "wrong MIME", contentType: "application/xhtml+xml", values: []string{"application/xhtml+xml"}, body: validBody},
		{name: "wrong charset", contentType: "text/html; charset=iso-8859-1", values: []string{"text/html; charset=iso-8859-1"}, body: validBody},
		{name: "extra parameter", contentType: "text/html; charset=utf-8; version=1", values: []string{"text/html; charset=utf-8; version=1"}, body: validBody},
		{name: "non UTF-8 body", contentType: "text/html", values: []string{"text/html"}, body: []byte{0xff}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validatePageHTMLResponse(test.contentType, test.values, test.body); err == nil {
				t.Fatal("invalid HTML response was accepted")
			}
		})
	}
}

func TestGetPageDataRejectsOversizedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(strings.Repeat("x", int(maxPageBodyBytes)+1)))
	}))
	defer server.Close()

	fetcher, policy := testPageFetcher(t, server)
	_, err := getPageData(
		context.Background(),
		fetcher,
		"http://example.com/",
		func(rawURL string) (crawlpolicy.Decision, error) { return policy.Match(rawURL, 0) },
		allowRequestGate{},
		nil,
	)
	if securefetch.ReasonOf(err) != securefetch.ReasonBodyTooLarge {
		t.Fatalf("expected response size error, got %v", err)
	}
}

func TestGetPageDataSendsPolicyUserAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("User-Agent"); got != "GroupBot/1.0" {
			http.Error(w, "unexpected user agent", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	}))
	defer server.Close()

	fetcher, policy := testPageFetcher(t, server)
	_, err := getPageData(
		context.Background(),
		fetcher,
		"http://example.com/",
		func(rawURL string) (crawlpolicy.Decision, error) { return policy.Match(rawURL, 0) },
		allowRequestGate{},
		nil,
	)
	if err != nil {
		t.Fatalf("getPageData failed: %v", err)
	}
}

type staticResolver struct{}

func (staticResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
}

type forwardingDialer struct {
	address string
}

func (dialer forwardingDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	connection, err := (&net.Dialer{}).DialContext(ctx, network, dialer.address)
	if err != nil {
		return nil, err
	}
	return &remoteAddressConn{
		Conn: connection,
		remote: &net.TCPAddr{
			IP:   net.ParseIP("93.184.216.34"),
			Port: 80,
		},
	}, nil
}

type remoteAddressConn struct {
	net.Conn
	remote net.Addr
}

func (connection *remoteAddressConn) RemoteAddr() net.Addr {
	return connection.remote
}

type allowRequestGate struct{}

func (allowRequestGate) Acquire(context.Context, crawlpolicy.Decision) (func(), error) {
	return func() {}, nil
}

func testPageFetcher(t *testing.T, server *httptest.Server) (*securefetch.Fetcher, *crawlpolicy.Policy) {
	t.Helper()
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, "")
	}

	serverAddress := strings.TrimPrefix(server.URL, "http://")
	fetcher, err := securefetch.New(securefetch.Config{
		Resolver:       staticResolver{},
		Dialer:         forwardingDialer{address: serverAddress},
		LocalAddresses: []netip.Addr{},
	})
	if err != nil {
		t.Fatalf("create secure fetcher: %v", err)
	}

	document := fmt.Sprintf(`{
		"schema_version": 1,
		"unmatched_action": "deny",
		"groups": [{
			"id": "pages",
			"enabled": true,
			"priority": 1,
			"host_rules": [{"host": "example.com", "match": "exact"}],
			"allowed_schemes": ["http"],
			"max_depth": 1,
			"allow_path_prefixes": [],
			"deny_path_prefixes": [],
			"min_request_interval": "0s",
			"max_concurrency": 1,
			"max_requests_per_batch": 10,
			"user_agent": %q,
			"redirects": {"mode": "none", "max_hops": 0},
			"robots": {"mode": "enforce", "on_error": "deny", "cache_ttl": "1h"}
		}]
	}`, "GroupBot/1.0")
	policy, err := crawlpolicy.Decode(strings.NewReader(document), "FallbackBot/1.0")
	if err != nil {
		t.Fatalf("decode test policy: %v", err)
	}
	return fetcher, policy
}
