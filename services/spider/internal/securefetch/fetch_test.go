package securefetch

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawlpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

func TestDNSPinningUsesOneSnapshotAndOnlyNumericApprovedAddresses(t *testing.T) {
	resolver := &recordingResolver{answers: map[string][]netip.Addr{
		"example.com": {
			netip.MustParseAddr("93.184.216.34"),
			netip.MustParseAddr("142.250.72.14"),
		},
	}}
	dialer := &scriptedDialer{
		fail: map[string]error{"93.184.216.34:80": errors.New("first address unavailable")},
		handler: func(request *http.Request) (responseScript, error) {
			if request.Method != http.MethodGet {
				return responseScript{}, fmt.Errorf("method = %q", request.Method)
			}
			if request.Host != "example.com" || request.URL.RequestURI() != "/page?public=1" {
				return responseScript{}, fmt.Errorf("authority/URI = %q %q", request.Host, request.URL.RequestURI())
			}
			if request.Header.Get("Accept-Encoding") != "identity" || request.Header.Get("User-Agent") != "MiFolyoTest/1.0" {
				return responseScript{}, fmt.Errorf("unexpected headers: %#v", request.Header)
			}
			return responseScript{
				status: http.StatusOK,
				header: http.Header{"Content-Type": {"text/html; charset=utf-8"}},
				body:   "hello",
			}, nil
		},
	}
	fetcher := newTestFetcher(t, resolver, dialer, nil)
	matcher := newMatchRecorder(crawlpolicy.RedirectSameHost, 2, map[string]string{"example.com": "pages"})
	gate := &countingGate{}

	result, err := fetcher.Fetch(context.Background(), "http://example.com/page?public=1", matcher.match, gate, 64)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	dialer.wait(t)
	if string(result.Body) != "hello" || result.StatusCode != http.StatusOK || result.ContentType != "text/html; charset=utf-8" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.EffectiveURL != "http://example.com/page?public=1" || result.Decision.Group.ID != "pages" || len(result.RedirectChain) != 0 {
		t.Fatalf("unexpected effective decision: %#v", result)
	}
	if got := resolver.hostCalls("example.com"); got != 1 {
		t.Fatalf("resolver calls = %d, want 1", got)
	}
	dials := dialer.dialSnapshot()
	wantDials := []string{"93.184.216.34:80", "142.250.72.14:80"}
	if len(dials) != len(wantDials) {
		t.Fatalf("dials = %#v, want %v", dials, wantDials)
	}
	for index, dial := range dials {
		if dial.network != "tcp" || dial.address != wantDials[index] {
			t.Fatalf("dial %d = %#v, want tcp %s", index, dial, wantDials[index])
		}
		if strings.Contains(dial.address, "example.com") {
			t.Fatalf("hostname reached underlying dialer: %q", dial.address)
		}
	}
	gate.assertCounts(t, 1, 1)
}

func TestMixedPublicAndPrivateDNSFailsBeforeAnyDial(t *testing.T) {
	resolver := &recordingResolver{answers: map[string][]netip.Addr{
		"example.com": {netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.7")},
	}}
	dialer := &scriptedDialer{}
	fetcher := newTestFetcher(t, resolver, dialer, nil)
	matcher := newMatchRecorder(crawlpolicy.RedirectSameHost, 1, map[string]string{"example.com": "pages"})
	gate := &countingGate{}

	_, err := fetcher.Fetch(context.Background(), "http://example.com/", matcher.match, gate, 64)
	assertReason(t, err, ReasonDNSProhibitedAddress)
	if len(dialer.dialSnapshot()) != 0 {
		t.Fatalf("mixed DNS answer caused dials: %#v", dialer.dialSnapshot())
	}
	if resolver.hostCalls("example.com") != 1 {
		t.Fatalf("resolver calls = %d", resolver.hostCalls("example.com"))
	}
	gate.assertCounts(t, 1, 1)
}

func TestDNSAnswerValidationFailsClosedWithoutDial(t *testing.T) {
	tooMany := make([]netip.Addr, maximumDNSAnswers+1)
	for index := range tooMany {
		tooMany[index] = netip.AddrFrom4([4]byte{11, 0, 0, byte(index + 1)})
	}
	tests := []struct {
		name    string
		answers []netip.Addr
		reason  Reason
	}{
		{name: "no answer", reason: ReasonDNSNoAnswer},
		{name: "too many", answers: tooMany, reason: ReasonDNSTooManyAnswers},
		{name: "duplicate", answers: []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("93.184.216.34")}, reason: ReasonDNSDuplicateAnswer},
		{name: "mapped duplicate", answers: []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("::ffff:93.184.216.34")}, reason: ReasonDNSDuplicateAnswer},
		{name: "zoned", answers: []netip.Addr{netip.MustParseAddr("fe80::1%en0")}, reason: ReasonDNSInvalidAnswer},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &recordingResolver{answers: map[string][]netip.Addr{"example.com": test.answers}}
			dialer := &scriptedDialer{}
			fetcher := newTestFetcher(t, resolver, dialer, nil)
			matcher := newMatchRecorder(crawlpolicy.RedirectSameHost, 1, map[string]string{"example.com": "pages"})
			gate := &countingGate{}

			_, err := fetcher.Fetch(context.Background(), "http://example.com/", matcher.match, gate, 64)
			assertReason(t, err, test.reason)
			if len(dialer.dialSnapshot()) != 0 {
				t.Fatalf("invalid answer caused a dial: %#v", dialer.dialSnapshot())
			}
			gate.assertCounts(t, 1, 1)
		})
	}
}

func TestGateDenialOccursBeforeDNS(t *testing.T) {
	resolver := &recordingResolver{answers: map[string][]netip.Addr{"example.com": {netip.MustParseAddr("93.184.216.34")}}}
	dialer := &scriptedDialer{}
	fetcher := newTestFetcher(t, resolver, dialer, nil)
	matcher := newMatchRecorder(crawlpolicy.RedirectSameHost, 1, map[string]string{"example.com": "pages"})
	gate := &countingGate{err: errors.New("budget exhausted")}

	_, err := fetcher.Fetch(context.Background(), "http://example.com/", matcher.match, gate, 64)
	assertReason(t, err, ReasonGateDenied)
	if resolver.totalCalls() != 0 || len(dialer.dialSnapshot()) != 0 {
		t.Fatalf("network activity occurred after gate denial: DNS=%d dials=%v", resolver.totalCalls(), dialer.dialSnapshot())
	}
}

func TestRedirectToPrivateAddressIsDeniedBeforeRedirectDial(t *testing.T) {
	resolver := &recordingResolver{answers: map[string][]netip.Addr{
		"start.example.com":   {netip.MustParseAddr("93.184.216.34")},
		"private.example.com": {netip.MustParseAddr("192.168.10.4")},
	}}
	dialer := &scriptedDialer{handler: func(request *http.Request) (responseScript, error) {
		return responseScript{
			status: http.StatusFound,
			header: http.Header{"Location": {"http://private.example.com/secret"}},
		}, nil
	}}
	fetcher := newTestFetcher(t, resolver, dialer, nil)
	matcher := newMatchRecorder(crawlpolicy.RedirectSameGroup, 3, map[string]string{
		"start.example.com":   "pages",
		"private.example.com": "pages",
	})
	gate := &countingGate{}

	_, err := fetcher.Fetch(context.Background(), "http://start.example.com/", matcher.match, gate, 64)
	assertReason(t, err, ReasonDNSProhibitedAddress)
	dialer.wait(t)
	if len(dialer.dialSnapshot()) != 1 {
		t.Fatalf("redirect target was dialed: %#v", dialer.dialSnapshot())
	}
	if matcher.callCount() != 2 || resolver.hostCalls("private.example.com") != 1 {
		t.Fatalf("target was not matched/resolved exactly once: matches=%d DNS=%d", matcher.callCount(), resolver.hostCalls("private.example.com"))
	}
	gate.assertCounts(t, 2, 2)
}

func TestRedirectModes(t *testing.T) {
	tests := []struct {
		name          string
		mode          crawlpolicy.RedirectMode
		location      string
		groups        map[string]string
		wantReason    Reason
		wantGateCalls int
	}{
		{
			name: "same host allows relative",
			mode: crawlpolicy.RedirectSameHost, location: "/final?x=1",
			groups: map[string]string{"a.example.com": "a"}, wantGateCalls: 2,
		},
		{
			name: "same host rejects another host",
			mode: crawlpolicy.RedirectSameHost, location: "http://b.example.com/final",
			groups:     map[string]string{"a.example.com": "a", "b.example.com": "a"},
			wantReason: ReasonRedirectHost, wantGateCalls: 1,
		},
		{
			name: "same group allows another host",
			mode: crawlpolicy.RedirectSameGroup, location: "http://b.example.com/final",
			groups: map[string]string{"a.example.com": "a", "b.example.com": "a"}, wantGateCalls: 2,
		},
		{
			name: "same group rejects another group",
			mode: crawlpolicy.RedirectSameGroup, location: "http://b.example.com/final",
			groups:     map[string]string{"a.example.com": "a", "b.example.com": "b"},
			wantReason: ReasonRedirectGroup, wantGateCalls: 1,
		},
		{
			name: "none rejects",
			mode: crawlpolicy.RedirectNone, location: "/final",
			groups:     map[string]string{"a.example.com": "a"},
			wantReason: ReasonRedirectMode, wantGateCalls: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &recordingResolver{answers: map[string][]netip.Addr{
				"a.example.com": {netip.MustParseAddr("93.184.216.34")},
				"b.example.com": {netip.MustParseAddr("142.250.72.14")},
			}}
			dialer := &scriptedDialer{handler: func(request *http.Request) (responseScript, error) {
				if request.URL.Path == "/start" {
					return responseScript{status: http.StatusFound, header: http.Header{"Location": {test.location}}}, nil
				}
				return responseScript{status: http.StatusOK, body: "done"}, nil
			}}
			fetcher := newTestFetcher(t, resolver, dialer, nil)
			matcher := newMatchRecorder(test.mode, 3, test.groups)
			gate := &countingGate{}

			result, err := fetcher.Fetch(context.Background(), "http://a.example.com/start", matcher.match, gate, 64)
			if test.wantReason != "" {
				assertReason(t, err, test.wantReason)
			} else {
				if err != nil {
					t.Fatalf("Fetch failed: %v", err)
				}
				if string(result.Body) != "done" || len(result.RedirectChain) != 1 || result.RedirectChain[0] != result.EffectiveURL {
					t.Fatalf("unexpected redirect result: %#v", result)
				}
			}
			dialer.wait(t)
			if matcher.callCount() != 2 {
				t.Fatalf("matcher calls = %d, want 2", matcher.callCount())
			}
			gate.assertCounts(t, test.wantGateCalls, test.wantGateCalls)
		})
	}
}

func TestEverySupportedRedirectStatusIsManualAndRemainsGET(t *testing.T) {
	statuses := []int{
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	}
	for _, status := range statuses {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			resolver := &recordingResolver{answers: map[string][]netip.Addr{"example.com": {netip.MustParseAddr("93.184.216.34")}}}
			dialer := &scriptedDialer{handler: func(request *http.Request) (responseScript, error) {
				if request.URL.Path == "/start" {
					return responseScript{status: status, header: http.Header{"Location": {"/final"}}}, nil
				}
				return responseScript{status: http.StatusOK, body: "ok"}, nil
			}}
			fetcher := newTestFetcher(t, resolver, dialer, nil)
			matcher := newMatchRecorder(crawlpolicy.RedirectSameHost, 1, map[string]string{"example.com": "pages"})
			gate := &countingGate{}

			result, err := fetcher.Fetch(context.Background(), "http://example.com/start", matcher.match, gate, 64)
			if err != nil || string(result.Body) != "ok" {
				t.Fatalf("redirect %d failed: result=%#v err=%v", status, result, err)
			}
			dialer.wait(t)
			for _, request := range dialer.requestSnapshot() {
				if request.method != http.MethodGet {
					t.Fatalf("redirect changed method to %q", request.method)
				}
			}
			gate.assertCounts(t, 2, 2)
		})
	}
}

func TestRedirectsDoNotRetainCookies(t *testing.T) {
	resolver := &recordingResolver{answers: map[string][]netip.Addr{"example.com": {netip.MustParseAddr("93.184.216.34")}}}
	dialer := &scriptedDialer{handler: func(request *http.Request) (responseScript, error) {
		if request.URL.Path == "/start" {
			return responseScript{
				status: http.StatusFound,
				header: http.Header{
					"Location":   {"/final"},
					"Set-Cookie": {"session=attacker-controlled; Path=/"},
				},
			}, nil
		}
		if cookie := request.Header.Get("Cookie"); cookie != "" {
			return responseScript{}, fmt.Errorf("redirect retained cookie %q", cookie)
		}
		return responseScript{status: http.StatusOK, body: "ok"}, nil
	}}
	fetcher := newTestFetcher(t, resolver, dialer, nil)
	matcher := newMatchRecorder(crawlpolicy.RedirectSameHost, 1, map[string]string{"example.com": "pages"})
	gate := &countingGate{}

	result, err := fetcher.Fetch(context.Background(), "http://example.com/start", matcher.match, gate, 64)
	if err != nil || string(result.Body) != "ok" {
		t.Fatalf("Fetch failed: result=%#v err=%v", result, err)
	}
	dialer.wait(t)
	gate.assertCounts(t, 2, 2)
}

func TestHTTPSDowngradeIsDeniedAndTLSPreservesSNI(t *testing.T) {
	serverTLS, roots := testTLSCertificate(t, "secure.example.com")
	serverNames := make(chan string, 1)
	serverTLS.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		serverNames <- hello.ServerName
		return nil, nil
	}
	resolver := &recordingResolver{answers: map[string][]netip.Addr{
		"secure.example.com": {netip.MustParseAddr("93.184.216.34")},
	}}
	dialer := &scriptedDialer{
		tlsConfig: serverTLS,
		handler: func(*http.Request) (responseScript, error) {
			return responseScript{
				status: http.StatusFound,
				header: http.Header{"Location": {"http://insecure.example.com/final"}},
			}, nil
		},
	}
	fetcher := newTestFetcher(t, resolver, dialer, roots)
	matcher := newMatchRecorder(crawlpolicy.RedirectSameGroup, 3, map[string]string{
		"secure.example.com":   "pages",
		"insecure.example.com": "pages",
	})
	gate := &countingGate{}

	_, err := fetcher.Fetch(context.Background(), "https://secure.example.com/start", matcher.match, gate, 64)
	assertReason(t, err, ReasonHTTPSDowngrade)
	dialer.wait(t)
	select {
	case serverName := <-serverNames:
		if serverName != "secure.example.com" {
			t.Fatalf("TLS SNI = %q", serverName)
		}
	default:
		t.Fatal("TLS ClientHello was not observed")
	}
	if matcher.callCount() != 2 || resolver.totalCalls() != 1 || len(dialer.dialSnapshot()) != 1 {
		t.Fatalf("downgrade performed network activity: matches=%d DNS=%d dials=%v", matcher.callCount(), resolver.totalCalls(), dialer.dialSnapshot())
	}
	gate.assertCounts(t, 1, 1)
}

func TestRedirectCanonicalCycleAndHopLimit(t *testing.T) {
	tests := []struct {
		name       string
		maxHops    int
		handler    func(*http.Request) (responseScript, error)
		wantReason Reason
	}{
		{
			name: "cycle", maxHops: 5, wantReason: ReasonRedirectCycle,
			handler: func(request *http.Request) (responseScript, error) {
				location := "/b"
				if request.URL.Path == "/b" {
					location = "/a"
				}
				return responseScript{status: http.StatusFound, header: http.Header{"Location": {location}}}, nil
			},
		},
		{
			name: "hop limit", maxHops: 1, wantReason: ReasonRedirectHopLimit,
			handler: func(request *http.Request) (responseScript, error) {
				next := "/1"
				if request.URL.Path == "/1" {
					next = "/2"
				}
				return responseScript{status: http.StatusFound, header: http.Header{"Location": {next}}}, nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &recordingResolver{answers: map[string][]netip.Addr{"example.com": {netip.MustParseAddr("93.184.216.34")}}}
			dialer := &scriptedDialer{handler: test.handler}
			fetcher := newTestFetcher(t, resolver, dialer, nil)
			matcher := newMatchRecorder(crawlpolicy.RedirectSameHost, test.maxHops, map[string]string{"example.com": "pages"})
			gate := &countingGate{}
			start := "http://example.com/0"
			if test.name == "cycle" {
				start = "http://example.com/a"
			}

			_, err := fetcher.Fetch(context.Background(), start, matcher.match, gate, 64)
			assertReason(t, err, test.wantReason)
			dialer.wait(t)
			if matcher.callCount() != 3 {
				t.Fatalf("matcher calls = %d, want 3", matcher.callCount())
			}
			gate.assertCounts(t, 2, 2)
			if resolver.totalCalls() != 2 {
				t.Fatalf("resolver calls = %d, want 2", resolver.totalCalls())
			}
		})
	}
}

func TestOversizedTraversalRedirectIsRejectedBeforeParsingOrMatching(t *testing.T) {
	resolver := &recordingResolver{answers: map[string][]netip.Addr{
		"example.com": {netip.MustParseAddr("93.184.216.34")},
	}}
	location := strings.Repeat("../", utils.MaxURLBytesV1/3+1)
	if len(location) <= utils.MaxURLBytesV1 {
		t.Fatalf("test Location length = %d, want more than %d", len(location), utils.MaxURLBytesV1)
	}
	dialer := &scriptedDialer{handler: func(*http.Request) (responseScript, error) {
		return responseScript{
			status: http.StatusFound,
			header: http.Header{"Location": {location}},
		}, nil
	}}
	fetcher := newTestFetcher(t, resolver, dialer, nil)
	matcher := newMatchRecorder(crawlpolicy.RedirectSameHost, 2, map[string]string{"example.com": "pages"})
	gate := &countingGate{}

	_, err := fetcher.Fetch(context.Background(), "http://example.com/start", matcher.match, gate, 64)
	assertReason(t, err, ReasonRedirectLocation)
	dialer.wait(t)
	if matcher.callCount() != 1 {
		t.Fatalf("matcher calls = %d, want only the initial target", matcher.callCount())
	}
	if resolver.totalCalls() != 1 || len(dialer.dialSnapshot()) != 1 {
		t.Fatalf("oversized redirect reached another hop: DNS=%d dials=%v", resolver.totalCalls(), dialer.dialSnapshot())
	}
	gate.assertCounts(t, 1, 1)
}

func TestEveryFollowedHopAcquiresAndReleasesGate(t *testing.T) {
	resolver := &recordingResolver{answers: map[string][]netip.Addr{"example.com": {netip.MustParseAddr("93.184.216.34")}}}
	dialer := &scriptedDialer{handler: func(request *http.Request) (responseScript, error) {
		switch request.URL.Path {
		case "/start":
			return responseScript{status: http.StatusMovedPermanently, header: http.Header{"Location": {"/middle"}}}, nil
		case "/middle":
			return responseScript{status: http.StatusPermanentRedirect, header: http.Header{"Location": {"/final"}}}, nil
		default:
			return responseScript{status: http.StatusOK, body: "complete"}, nil
		}
	}}
	fetcher := newTestFetcher(t, resolver, dialer, nil)
	matcher := newMatchRecorder(crawlpolicy.RedirectSameHost, 2, map[string]string{"example.com": "pages"})
	gate := &countingGate{}

	result, err := fetcher.Fetch(context.Background(), "http://example.com/start", matcher.match, gate, 64)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	dialer.wait(t)
	if string(result.Body) != "complete" || len(result.RedirectChain) != 2 {
		t.Fatalf("unexpected result: %#v", result)
	}
	gate.assertCounts(t, 3, 3)
	if resolver.totalCalls() != 3 || len(dialer.dialSnapshot()) != 3 || matcher.callCount() != 3 {
		t.Fatalf("hop counts differ: matches=%d DNS=%d dials=%d", matcher.callCount(), resolver.totalCalls(), len(dialer.dialSnapshot()))
	}
}

func TestHopAuthorizerRunsBeforeEveryRedirectRequest(t *testing.T) {
	resolver := &recordingResolver{answers: map[string][]netip.Addr{"example.com": {netip.MustParseAddr("93.184.216.34")}}}
	dialer := &scriptedDialer{handler: func(request *http.Request) (responseScript, error) {
		return responseScript{status: http.StatusFound, header: http.Header{"Location": {"/private"}}}, nil
	}}
	fetcher := newTestFetcher(t, resolver, dialer, nil)
	matcher := newMatchRecorder(crawlpolicy.RedirectSameHost, 2, map[string]string{"example.com": "pages"})
	gate := &countingGate{}
	var authorized []string
	denied := errors.New("robots denied redirect target")
	authorizer := func(_ context.Context, decision crawlpolicy.Decision, _ RequestGate) error {
		authorized = append(authorized, decision.Path)
		if decision.Path == "/private" {
			return denied
		}
		return nil
	}

	_, err := fetcher.FetchAuthorized(context.Background(), "http://example.com/start", matcher.match, gate, 64, authorizer)
	assertReason(t, err, ReasonHopDenied)
	if !errors.Is(err, denied) {
		t.Fatalf("hop denial cause was not preserved: %v", err)
	}
	dialer.wait(t)
	if strings.Join(authorized, ",") != "/start,/private" {
		t.Fatalf("authorized paths = %v", authorized)
	}
	if len(dialer.dialSnapshot()) != 1 || resolver.totalCalls() != 1 {
		t.Fatalf("denied redirect reached network: DNS=%d dials=%v", resolver.totalCalls(), dialer.dialSnapshot())
	}
	gate.assertCounts(t, 1, 1)
}

func TestFinalBodyLimitClosesAndReleases(t *testing.T) {
	resolver := &recordingResolver{answers: map[string][]netip.Addr{"example.com": {netip.MustParseAddr("93.184.216.34")}}}
	dialer := &scriptedDialer{handler: func(*http.Request) (responseScript, error) {
		return responseScript{status: http.StatusOK, body: "12345", chunked: true, allowClientClose: true}, nil
	}}
	fetcher := newTestFetcher(t, resolver, dialer, nil)
	matcher := newMatchRecorder(crawlpolicy.RedirectSameHost, 1, map[string]string{"example.com": "pages"})
	gate := &countingGate{}

	_, err := fetcher.Fetch(context.Background(), "http://example.com/", matcher.match, gate, 4)
	assertReason(t, err, ReasonBodyTooLarge)
	dialer.wait(t)
	gate.assertCounts(t, 1, 1)
}

func TestProtocolSwitchIsRejected(t *testing.T) {
	resolver := &recordingResolver{answers: map[string][]netip.Addr{"example.com": {netip.MustParseAddr("93.184.216.34")}}}
	dialer := &scriptedDialer{handler: func(*http.Request) (responseScript, error) {
		return responseScript{status: http.StatusSwitchingProtocols}, nil
	}}
	fetcher := newTestFetcher(t, resolver, dialer, nil)
	matcher := newMatchRecorder(crawlpolicy.RedirectNone, 0, map[string]string{"example.com": "pages"})
	gate := &countingGate{}

	_, err := fetcher.Fetch(context.Background(), "http://example.com/", matcher.match, gate, 64)
	assertReason(t, err, ReasonInvalidResponse)
	dialer.wait(t)
	gate.assertCounts(t, 1, 1)
}

func TestFinalResponseContentEncoding(t *testing.T) {
	tests := []struct {
		name      string
		encodings []string
		allowed   bool
	}{
		{name: "absent", allowed: true},
		{name: "single identity", encodings: []string{"identity"}, allowed: true},
		{name: "trimmed case-insensitive identity", encodings: []string{" Identity "}, allowed: true},
		{name: "gzip", encodings: []string{"gzip"}},
		{name: "empty coding", encodings: []string{""}},
		{name: "multiple identity fields", encodings: []string{"identity", "identity"}},
		{name: "coding list", encodings: []string{"identity, gzip"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &recordingResolver{answers: map[string][]netip.Addr{"example.com": {netip.MustParseAddr("93.184.216.34")}}}
			dialer := &scriptedDialer{handler: func(*http.Request) (responseScript, error) {
				header := make(http.Header)
				if test.encodings != nil {
					header["Content-Encoding"] = append([]string(nil), test.encodings...)
				}
				return responseScript{status: http.StatusOK, header: header, body: "ok", allowClientClose: !test.allowed}, nil
			}}
			fetcher := newTestFetcher(t, resolver, dialer, nil)
			matcher := newMatchRecorder(crawlpolicy.RedirectNone, 0, map[string]string{"example.com": "pages"})
			gate := &countingGate{}

			result, err := fetcher.Fetch(context.Background(), "http://example.com/", matcher.match, gate, 64)
			if test.allowed {
				if err != nil || string(result.Body) != "ok" {
					t.Fatalf("identity response failed: result=%#v err=%v", result, err)
				}
			} else {
				assertReason(t, err, ReasonContentEncoding)
			}
			dialer.wait(t)
			gate.assertCounts(t, 1, 1)
		})
	}
}

func TestFetcherReenforcesAuthorityWithPermissiveMatcher(t *testing.T) {
	resolver := &recordingResolver{}
	dialer := &scriptedDialer{}
	fetcher := newTestFetcher(t, resolver, dialer, nil)
	gate := &countingGate{}
	matcher := func(raw string) (crawlpolicy.Decision, error) {
		identity, err := utils.CanonicalizeURLV1(raw)
		if err != nil {
			return crawlpolicy.Decision{}, err
		}
		parsed, err := url.Parse(identity.CanonicalURL)
		if err != nil {
			return crawlpolicy.Decision{}, err
		}
		return crawlpolicy.Decision{
			Identity: identity,
			URL:      parsed,
			Scheme:   parsed.Scheme,
			Host:     parsed.Hostname(),
			Path:     parsed.EscapedPath(),
			Group: crawlpolicy.Group{
				ID:        "pages",
				Redirects: crawlpolicy.RedirectPolicy{Mode: crawlpolicy.RedirectNone, MaxHops: 0},
			},
		}, nil
	}

	_, err := fetcher.Fetch(context.Background(), "http://example.com:8080/path?token=secret", matcher, gate, 64)
	assertReason(t, err, ReasonNonDefaultPort)
	if resolver.totalCalls() != 0 || len(dialer.dialSnapshot()) != 0 {
		t.Fatal("non-default authority reached the network")
	}
}

func TestFetcherRejectsAmbiguousPathsWithPermissiveMatcher(t *testing.T) {
	resolver := &recordingResolver{}
	dialer := &scriptedDialer{}
	fetcher := newTestFetcher(t, resolver, dialer, nil)
	gate := &countingGate{}
	matcher := func(raw string) (crawlpolicy.Decision, error) {
		identity, err := utils.CanonicalizeURLV1(raw)
		if err != nil {
			return crawlpolicy.Decision{}, err
		}
		parsed, err := url.Parse(identity.CanonicalURL)
		if err != nil {
			return crawlpolicy.Decision{}, err
		}
		return crawlpolicy.Decision{
			Identity: identity,
			URL:      parsed,
			Scheme:   parsed.Scheme,
			Host:     parsed.Hostname(),
			Path:     parsed.EscapedPath(),
			Group: crawlpolicy.Group{
				ID:        "pages",
				Redirects: crawlpolicy.RedirectPolicy{Mode: crawlpolicy.RedirectNone},
			},
		}, nil
	}

	for _, path := range []string{
		"/%70rivate",
		"/public%2F..%2Fprivate",
		"/public//private",
		"/a/../private",
		"/%5Cprivate",
		"/%25value",
		"/public/%252E%252E/private",
		"/%FF",
		"/%C0%AFprivate",
	} {
		_, err := fetcher.Fetch(context.Background(), "http://example.com"+path, matcher, gate, 64)
		assertReason(t, err, ReasonAmbiguousPath)
	}
	if resolver.totalCalls() != 0 || len(dialer.dialSnapshot()) != 0 {
		t.Fatal("ambiguous path reached DNS or dial")
	}
}

func TestErrorStringRedactsMatcherURLQuery(t *testing.T) {
	resolver := &recordingResolver{}
	fetcher := newTestFetcher(t, resolver, &scriptedDialer{}, nil)
	matcher := func(string) (crawlpolicy.Decision, error) {
		return crawlpolicy.Decision{}, errors.New("denied https://example.com/?token=extremely-sensitive")
	}
	_, err := fetcher.Fetch(context.Background(), "https://example.com/?token=extremely-sensitive", matcher, &countingGate{}, 64)
	assertReason(t, err, ReasonMatcherDenied)
	if strings.Contains(err.Error(), "token") || strings.Contains(err.Error(), "extremely-sensitive") {
		t.Fatalf("sensitive query leaked: %q", err)
	}
}

func newTestFetcher(t *testing.T, resolver Resolver, dialer Dialer, roots *x509.CertPool) *Fetcher {
	t.Helper()
	clearProxyEnvironment(t)
	fetcher, err := New(Config{
		Resolver:              resolver,
		Dialer:                dialer,
		RootCAs:               roots,
		LocalAddresses:        []netip.Addr{},
		DNSLookupTimeout:      2 * time.Second,
		DialTimeout:           2 * time.Second,
		TLSHandshakeTimeout:   2 * time.Second,
		ResponseHeaderTimeout: 2 * time.Second,
		TotalTimeout:          5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	return fetcher
}

type matchRecorder struct {
	mu      sync.Mutex
	mode    crawlpolicy.RedirectMode
	maxHops int
	groups  map[string]string
	calls   []string
}

func newMatchRecorder(mode crawlpolicy.RedirectMode, maxHops int, groups map[string]string) *matchRecorder {
	return &matchRecorder{mode: mode, maxHops: maxHops, groups: groups}
}

func (matcher *matchRecorder) match(raw string) (crawlpolicy.Decision, error) {
	matcher.mu.Lock()
	matcher.calls = append(matcher.calls, raw)
	matcher.mu.Unlock()

	identity, err := utils.CanonicalizeURLV1(raw)
	if err != nil {
		return crawlpolicy.Decision{}, err
	}
	if err := utils.RequireStaticCrawlEligibility(identity); err != nil {
		return crawlpolicy.Decision{}, err
	}
	parsed, err := url.Parse(identity.CanonicalURL)
	if err != nil {
		return crawlpolicy.Decision{}, err
	}
	groupID, ok := matcher.groups[parsed.Hostname()]
	if !ok {
		return crawlpolicy.Decision{}, errors.New("host not matched")
	}
	return crawlpolicy.Decision{
		Identity: identity,
		URL:      parsed,
		Scheme:   parsed.Scheme,
		Host:     parsed.Hostname(),
		Path:     parsed.EscapedPath(),
		Group: crawlpolicy.Group{
			ID:        groupID,
			UserAgent: "MiFolyoTest/1.0",
			Redirects: crawlpolicy.RedirectPolicy{Mode: matcher.mode, MaxHops: matcher.maxHops},
		},
	}, nil
}

func (matcher *matchRecorder) callCount() int {
	matcher.mu.Lock()
	defer matcher.mu.Unlock()
	return len(matcher.calls)
}

type countingGate struct {
	mu       sync.Mutex
	err      error
	acquires int
	releases int
	active   int
}

func (gate *countingGate) Acquire(ctx context.Context, _ crawlpolicy.Decision) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	gate.mu.Lock()
	gate.acquires++
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

func (gate *countingGate) assertCounts(t *testing.T, acquires, releases int) {
	t.Helper()
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.acquires != acquires || gate.releases != releases || gate.active != 0 {
		t.Fatalf("gate counts = acquire:%d release:%d active:%d, want %d/%d/0", gate.acquires, gate.releases, gate.active, acquires, releases)
	}
}

type recordingResolver struct {
	mu      sync.Mutex
	answers map[string][]netip.Addr
	errors  map[string]error
	calls   []string
}

func (resolver *recordingResolver) LookupNetIP(_ context.Context, network, host string) ([]netip.Addr, error) {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	resolver.calls = append(resolver.calls, host)
	if network != "ip" {
		return nil, fmt.Errorf("network = %q", network)
	}
	if err := resolver.errors[host]; err != nil {
		return nil, err
	}
	return append([]netip.Addr(nil), resolver.answers[host]...), nil
}

func (resolver *recordingResolver) hostCalls(host string) int {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	count := 0
	for _, call := range resolver.calls {
		if call == host {
			count++
		}
	}
	return count
}

func (resolver *recordingResolver) totalCalls() int {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	return len(resolver.calls)
}

type responseScript struct {
	status           int
	header           http.Header
	body             string
	chunked          bool
	allowClientClose bool
}

type dialObservation struct {
	network string
	address string
}

type requestObservation struct {
	method string
	host   string
	uri    string
}

type scriptedDialer struct {
	mu             sync.Mutex
	fail           map[string]error
	remoteOverride map[string]netip.AddrPort
	tlsConfig      *tls.Config
	handler        func(*http.Request) (responseScript, error)
	dials          []dialObservation
	requests       []requestObservation
	done           []<-chan error
}

func (dialer *scriptedDialer) DialContext(_ context.Context, network, address string) (net.Conn, error) {
	dialer.mu.Lock()
	dialer.dials = append(dialer.dials, dialObservation{network: network, address: address})
	failure := dialer.fail[address]
	remoteOverride, hasRemoteOverride := dialer.remoteOverride[address]
	handler := dialer.handler
	tlsConfig := dialer.tlsConfig
	dialer.mu.Unlock()
	if failure != nil {
		return nil, failure
	}

	selected, err := netip.ParseAddrPort(address)
	if err != nil {
		return nil, fmt.Errorf("underlying dial received nonnumeric address: %w", err)
	}
	remote := selected
	if hasRemoteOverride {
		remote = remoteOverride
	}
	client, server := net.Pipe()
	done := make(chan error, 1)
	dialer.mu.Lock()
	dialer.done = append(dialer.done, done)
	dialer.mu.Unlock()

	go func() {
		defer server.Close()
		var serverConnection net.Conn = server
		if tlsConfig != nil {
			serverConnection = tls.Server(server, tlsConfig.Clone())
		}
		request, readErr := http.ReadRequest(bufio.NewReader(serverConnection))
		if readErr != nil {
			done <- readErr
			return
		}
		defer request.Body.Close()
		dialer.mu.Lock()
		dialer.requests = append(dialer.requests, requestObservation{method: request.Method, host: request.Host, uri: request.URL.RequestURI()})
		dialer.mu.Unlock()

		if handler == nil {
			done <- errors.New("no scripted response")
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
		if script.chunked {
			response.ContentLength = -1
			response.TransferEncoding = []string{"chunked"}
		}
		writeErr := response.Write(serverConnection)
		if script.allowClientClose && (errors.Is(writeErr, io.ErrClosedPipe) || errors.Is(writeErr, net.ErrClosed)) {
			writeErr = nil
		}
		done <- writeErr
	}()

	return &trackedRemoteConn{Conn: client, remote: net.TCPAddrFromAddrPort(remote)}, nil
}

func (dialer *scriptedDialer) dialSnapshot() []dialObservation {
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	return append([]dialObservation(nil), dialer.dials...)
}

func (dialer *scriptedDialer) requestSnapshot() []requestObservation {
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	return append([]requestObservation(nil), dialer.requests...)
}

func (dialer *scriptedDialer) wait(t *testing.T) {
	t.Helper()
	dialer.mu.Lock()
	doneChannels := append([]<-chan error(nil), dialer.done...)
	dialer.mu.Unlock()
	for _, done := range doneChannels {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("scripted server failed: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for scripted server")
		}
	}
}

func testTLSCertificate(t *testing.T, hostname string) (*tls.Config, *x509.CertPool) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkixName("securefetch test"),
		DNSNames:              []string{hostname},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatalf("CreateCertificate failed: %v", err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ParseCertificate failed: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: privateKey}},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"http/1.1"},
	}, roots
}

// pkixName is kept tiny to make certificate setup readable at call sites.
func pkixName(commonName string) pkix.Name {
	return pkix.Name{CommonName: commonName}
}
