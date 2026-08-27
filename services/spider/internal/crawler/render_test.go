package crawler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/IonelPopJara/search-engine/services/spider/internal/renderclient"
	"github.com/IonelPopJara/search-engine/services/spider/internal/renderpolicy"
)

type fakeRenderer struct {
	result renderclient.Result
	err    error
	jobs   []renderclient.Job
}

func (renderer *fakeRenderer) Render(_ context.Context, job renderclient.Job) (renderclient.Result, error) {
	renderer.jobs = append(renderer.jobs, job)
	return renderer.result, renderer.err
}

func testRenderPolicy(t *testing.T) *renderpolicy.Policy {
	t.Helper()
	policy, err := renderpolicy.Decode(strings.NewReader(`{
		"schema_version": 1,
		"default_action": "deny",
		"rules": [{
			"id": "inline-fixture",
			"enabled": true,
			"host_rule": {"host": "www.reddit.com", "match": "exact"},
			"allow_paths": ["/r/games"],
			"allow_path_prefixes": [],
			"deny_path_prefixes": [],
			"mode": "inline_only",
			"failure_action": "reject_page",
			"resource_rules": [],
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
				"max_resource_requests": 0,
				"max_aggregate_resource_bytes": 0,
				"max_resource_body_bytes": 0,
				"max_rendered_dom_bytes": 1048576,
				"max_dom_nodes": 1000,
				"max_redirect_hops": 0,
				"max_console_bytes": 1024
			}
		}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func TestRenderIfRequiredLeavesHTTPPageStatic(t *testing.T) {
	renderer := &fakeRenderer{result: renderclient.Result{HTML: "unexpected"}}
	crawler := &CrawlerConfig{RenderPolicy: testRenderPolicy(t), Renderer: renderer}
	result, err := crawler.renderIfRequired(context.Background(), "http://www.reddit.com/r/games", "static", nil)
	if err != nil || result.HTML != "static" || result.RuleID != "" || len(renderer.jobs) != 0 {
		t.Fatalf("unmatched render result=%#v err=%v jobs=%d", result, err, len(renderer.jobs))
	}
}

func TestRenderIfRequiredRejectsInvalidSourceAndMissingDigestBeforeWorker(t *testing.T) {
	for name, fixture := range map[string]struct {
		sourceHTML string
		digest     string
	}{
		"invalid UTF-8":  {sourceHTML: string([]byte{'<', 0xff, '>'}), digest: strings.Repeat("a", 64)},
		"missing digest": {sourceHTML: "<html></html>"},
	} {
		t.Run(name, func(t *testing.T) {
			renderer := &fakeRenderer{}
			crawler := &CrawlerConfig{
				RenderPolicy:       testRenderPolicy(t),
				RenderPolicySHA256: fixture.digest,
				Renderer:           renderer,
			}
			if _, err := crawler.renderIfRequired(context.Background(), "https://www.reddit.com/r/games", fixture.sourceHTML, nil); err == nil {
				t.Fatal("invalid render input was accepted")
			}
			if len(renderer.jobs) != 0 {
				t.Fatalf("renderer received %d invalid jobs", len(renderer.jobs))
			}
		})
	}
}

func TestCrawlPublishesRenderedArtifactAndPreservesOriginal(t *testing.T) {
	crawler, db := newHermeticRedditTestCrawler(t, renderTestHandler())
	renderer := &fakeRenderer{result: renderclient.Result{
		HTML:     `<html><body><main>rendered fixture<a href="/rendered-link">next</a></main></body></html>`,
		DOMNodes: 4,
	}}
	crawler.RenderPolicy = testRenderPolicy(t)
	crawler.RenderPolicySHA256 = strings.Repeat("a", 64)
	crawler.Renderer = renderer
	if err := db.PushURLWithDepth("https://www.reddit.com/r/games", 0, 0); err != nil {
		t.Fatal(err)
	}
	crawler.Wg.Add(1)
	crawler.Crawl(db)
	crawler.Wg.Wait()

	page := crawler.Pages["https://www.reddit.com/r/games"]
	if page == nil || !page.Rendered || page.RenderPolicyRule != "inline-fixture" ||
		page.RenderPolicySHA256 != strings.Repeat("a", 64) {
		t.Fatalf("rendered page = %#v", page)
	}
	if !strings.Contains(page.OriginalHTML, "static fixture") || !strings.Contains(page.HTML, "rendered fixture") {
		t.Fatalf("page artifacts were not separated: %#v", page)
	}
	if node := crawler.Outlinks["https://www.reddit.com/r/games"]; node == nil {
		t.Fatal("rendered links were not extracted")
	}
	if len(renderer.jobs) != 1 {
		t.Fatalf("render jobs = %d", len(renderer.jobs))
	}
	if renderer.jobs[0].Broker != nil {
		t.Fatal("inline render unexpectedly received a resource broker")
	}
}

func TestRenderFailurePublishesNothing(t *testing.T) {
	crawler, db := newHermeticRedditTestCrawler(t, renderTestHandler())
	crawler.RenderPolicy = testRenderPolicy(t)
	crawler.RenderPolicySHA256 = strings.Repeat("a", 64)
	crawler.Renderer = &fakeRenderer{err: errors.New("synthetic renderer failure")}
	const target = "https://www.reddit.com/r/games"
	if err := db.PushURLWithDepth(target, 0, 0); err != nil {
		t.Fatal(err)
	}
	crawler.Wg.Add(1)
	crawler.Crawl(db)
	crawler.Wg.Wait()

	if len(crawler.Pages) != 0 || len(crawler.Outlinks) != 0 || len(crawler.Images) != 0 {
		t.Fatalf("failed render leaked output: pages=%d links=%d images=%d", len(crawler.Pages), len(crawler.Outlinks), len(crawler.Images))
	}
	visited, err := db.HasURLBeenVisited(target)
	if err != nil {
		t.Fatal(err)
	}
	if visited {
		t.Fatal("failed render was marked visited")
	}
	pending, err := db.ListPendingURLs()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("terminal render failure was requeued: %#v", pending)
	}
}

func TestTransientRenderFailureRequeuesOriginalCandidate(t *testing.T) {
	crawler, db := newHermeticRedditTestCrawler(t, renderTestHandler())
	crawler.RenderPolicy = testRenderPolicy(t)
	crawler.RenderPolicySHA256 = strings.Repeat("a", 64)
	crawler.Renderer = &fakeRenderer{err: &renderclient.Error{
		Code:      "worker_busy",
		Temporary: true,
	}}
	const target = "https://www.reddit.com/r/games"
	if err := db.PushURLWithDepth(target, 2, 0); err != nil {
		t.Fatal(err)
	}
	crawler.Wg.Add(1)
	crawler.Crawl(db)
	crawler.Wg.Wait()

	pending, err := db.ListPendingURLs()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].CanonicalURL != target || pending[0].Score != 2 || pending[0].Depth != 0 {
		t.Fatalf("requeued candidates = %#v", pending)
	}
}

func renderTestHandler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/robots.txt":
			writer.Header().Set("Content-Type", "text/plain")
			_, _ = writer.Write([]byte("User-agent: *\nAllow: /\n"))
		case "/r/games":
			writer.Header().Set("Content-Type", "text/html")
			_, _ = writer.Write([]byte("<html><body><main id='root'>static fixture</main></body></html>"))
		default:
			http.NotFound(writer, request)
		}
	})
}
