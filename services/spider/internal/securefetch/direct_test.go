package securefetch

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"slices"
	"testing"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawlpolicy"
)

func TestFetchAuthorizedDirectReturnsFinalResponseAndContentTypeValues(t *testing.T) {
	contentTypes := []string{"text/html; charset=utf-8", "application/xhtml+xml"}
	responseHeader := http.Header{"Content-Type": contentTypes}
	resolver := &recordingResolver{answers: map[string][]netip.Addr{
		"example.com": {netip.MustParseAddr("93.184.216.34")},
	}}
	dialer := &scriptedDialer{handler: func(*http.Request) (responseScript, error) {
		return responseScript{
			status: http.StatusOK,
			header: responseHeader,
			body:   "direct",
		}, nil
	}}
	fetcher := newTestFetcher(t, resolver, dialer, nil)
	matcher := newMatchRecorder(crawlpolicy.RedirectSameHost, 2, map[string]string{"example.com": "pages"})
	gate := &countingGate{}
	authorized := 0
	authorizer := func(context.Context, crawlpolicy.Decision, RequestGate) error {
		authorized++
		return nil
	}

	result, err := fetcher.FetchAuthorizedDirect(
		context.Background(),
		"http://example.com/page",
		matcher.match,
		gate,
		64,
		authorizer,
	)
	if err != nil {
		t.Fatalf("FetchAuthorizedDirect failed: %v", err)
	}
	dialer.wait(t)
	if result.StatusCode != http.StatusOK || string(result.Body) != "direct" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.ContentType != contentTypes[0] {
		t.Fatalf("ContentType = %q, want %q", result.ContentType, contentTypes[0])
	}
	if !slices.Equal(result.ContentTypeValues, contentTypes) {
		t.Fatalf("ContentTypeValues = %#v, want %#v", result.ContentTypeValues, contentTypes)
	}
	if matcher.callCount() != 1 || authorized != 1 || resolver.totalCalls() != 1 {
		t.Fatalf("initial activity = matcher:%d authorizer:%d DNS:%d, want 1/1/1", matcher.callCount(), authorized, resolver.totalCalls())
	}
	gate.assertCounts(t, 1, 1)

	contentTypes[0] = "source/mutated"
	if result.ContentTypeValues[0] != "text/html; charset=utf-8" {
		t.Fatalf("ContentTypeValues aliases response header values: %#v", result.ContentTypeValues)
	}
	result.ContentTypeValues[1] = "result/mutated"
	if responseHeader.Values("Content-Type")[1] != "application/xhtml+xml" {
		t.Fatalf("response header values alias ContentTypeValues: %#v", responseHeader.Values("Content-Type"))
	}
}

func TestFetchAuthorizedDirectRejectsEveryRedirectBeforeTargetActivity(t *testing.T) {
	statuses := []int{
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	}
	for _, status := range statuses {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			resolver := &recordingResolver{answers: map[string][]netip.Addr{
				"start.example.com":  {netip.MustParseAddr("93.184.216.34")},
				"target.example.com": {netip.MustParseAddr("142.250.72.14")},
			}}
			dialer := &scriptedDialer{handler: func(*http.Request) (responseScript, error) {
				return responseScript{
					status: status,
					header: http.Header{"Location": {"http://target.example.com/final"}},
				}, nil
			}}
			fetcher := newTestFetcher(t, resolver, dialer, nil)
			matcher := newMatchRecorder(crawlpolicy.RedirectSameGroup, 3, map[string]string{
				"start.example.com":  "pages",
				"target.example.com": "pages",
			})
			gate := &countingGate{}
			authorized := make([]string, 0, 1)
			authorizer := func(_ context.Context, decision crawlpolicy.Decision, _ RequestGate) error {
				authorized = append(authorized, decision.Host)
				return nil
			}

			_, err := fetcher.FetchAuthorizedDirect(
				context.Background(),
				"http://start.example.com/start",
				matcher.match,
				gate,
				64,
				authorizer,
			)
			assertReason(t, err, ReasonRedirectHopLimit)
			dialer.wait(t)
			if matcher.callCount() != 1 {
				t.Fatalf("matcher calls = %d, want only the initial target", matcher.callCount())
			}
			if !slices.Equal(authorized, []string{"start.example.com"}) {
				t.Fatalf("authorized hosts = %#v, want only the initial target", authorized)
			}
			if resolver.totalCalls() != 1 || resolver.hostCalls("target.example.com") != 0 {
				t.Fatalf("DNS calls = %d (target %d), want 1 (target 0)", resolver.totalCalls(), resolver.hostCalls("target.example.com"))
			}
			if len(dialer.dialSnapshot()) != 1 {
				t.Fatalf("dials = %#v, want only the initial target", dialer.dialSnapshot())
			}
			gate.assertCounts(t, 1, 1)
		})
	}
}
