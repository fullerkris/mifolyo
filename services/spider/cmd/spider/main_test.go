package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IonelPopJara/search-engine/services/spider/internal/controllers"
	"github.com/IonelPopJara/search-engine/services/spider/internal/crawler"
	"github.com/IonelPopJara/search-engine/services/spider/internal/crawlpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/pages"
	"github.com/IonelPopJara/search-engine/services/spider/internal/renderpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

type testBatchSaver struct {
	pageErr    error
	linkErr    error
	imageErr   error
	publishErr error
	published  bool
	calls      []string
}

type transientBatchSaver struct {
	failures              map[string]int
	commitPublishFailures int
	failureErr            error
	published             bool
	calls                 []string
}

func (saver *transientBatchSaver) result(phase string) error {
	saver.calls = append(saver.calls, phase)
	if saver.failures[phase] > 0 {
		saver.failures[phase]--
		if saver.failureErr != nil {
			return saver.failureErr
		}
		return errors.New(phase + " unavailable")
	}
	return nil
}

func (saver *transientBatchSaver) SavePages(*crawler.CrawlerConfig) error {
	return saver.result("pages")
}

func (saver *transientBatchSaver) SaveLinks(*crawler.CrawlerConfig) error {
	return saver.result("links")
}

func (saver *transientBatchSaver) SaveImages(*crawler.CrawlerConfig) error {
	return saver.result("images")
}

func (saver *transientBatchSaver) PublishPages(*crawler.CrawlerConfig) error {
	saver.calls = append(saver.calls, "publish")
	if saver.commitPublishFailures > 0 {
		saver.commitPublishFailures--
		saver.published = true
		return errors.New("publication acknowledgement lost")
	}
	if saver.failures["publish"] > 0 {
		saver.failures["publish"]--
		return errors.New("publish unavailable")
	}
	saver.published = true
	return nil
}

func (saver *transientBatchSaver) BatchPublished(*crawler.CrawlerConfig) (bool, error) {
	return saver.published, nil
}

func (saver *testBatchSaver) SavePages(*crawler.CrawlerConfig) error {
	saver.calls = append(saver.calls, "pages")
	return saver.pageErr
}

func (saver *testBatchSaver) SaveLinks(*crawler.CrawlerConfig) error {
	saver.calls = append(saver.calls, "links")
	return saver.linkErr
}

func (saver *testBatchSaver) SaveImages(*crawler.CrawlerConfig) error {
	saver.calls = append(saver.calls, "images")
	return saver.imageErr
}

func (saver *testBatchSaver) PublishPages(*crawler.CrawlerConfig) error {
	saver.calls = append(saver.calls, "publish")
	if saver.publishErr == nil {
		saver.published = true
	}
	return saver.publishErr
}

func (saver *testBatchSaver) BatchPublished(*crawler.CrawlerConfig) (bool, error) {
	return saver.published, nil
}

func TestPersistAndResetBatchRetainsMemoryOnPersistenceFailure(t *testing.T) {
	page := pages.CreatePage("https://example.com/", "<html></html>", "text/html", 200)
	crawcfg := &crawler.CrawlerConfig{
		Mu:           &sync.Mutex{},
		Pages:        map[string]*pages.Page{page.NormalizedURL: page},
		Outlinks:     make(map[string]*pages.PageNode),
		Backlinks:    make(map[string]*pages.PageNode),
		Images:       make(map[string][]*pages.Image),
		Aliases:      map[string]int{page.NormalizedURL: 0},
		PageAttempts: 1,
	}
	saver := &testBatchSaver{pageErr: errors.New("redis unavailable")}

	if err := persistAndResetBatch(crawcfg, saver, saver, saver, saver); err == nil {
		t.Fatal("persistAndResetBatch returned nil")
	}
	if len(crawcfg.Pages) != 1 || len(crawcfg.Aliases) != 1 || crawcfg.PageAttempts != 1 {
		t.Fatalf("failed batch was cleared: pages=%d aliases=%d attempts=%d", len(crawcfg.Pages), len(crawcfg.Aliases), crawcfg.PageAttempts)
	}
	if !reflect.DeepEqual(saver.calls, []string{"pages"}) {
		t.Fatalf("calls after first phase failure = %v", saver.calls)
	}
}

func TestPersistAndResetBatchClearsMemoryAfterSuccess(t *testing.T) {
	page := pages.CreatePage("https://example.com/", "<html></html>", "text/html", 200)
	crawcfg := &crawler.CrawlerConfig{
		Mu:           &sync.Mutex{},
		Pages:        map[string]*pages.Page{page.NormalizedURL: page},
		Outlinks:     make(map[string]*pages.PageNode),
		Backlinks:    make(map[string]*pages.PageNode),
		Images:       make(map[string][]*pages.Image),
		Aliases:      map[string]int{page.NormalizedURL: 0},
		PageAttempts: 1,
	}
	saver := &testBatchSaver{}

	if err := persistAndResetBatch(crawcfg, saver, saver, saver, saver); err != nil {
		t.Fatal(err)
	}
	if len(crawcfg.Pages) != 0 || len(crawcfg.Aliases) != 0 || crawcfg.PageAttempts != 0 || crawcfg.BatchError != nil {
		t.Fatalf("successful batch was not reset: %#v", crawcfg)
	}
	if !reflect.DeepEqual(saver.calls, []string{"pages", "links", "images", "publish"}) {
		t.Fatalf("persistence/publication order = %v", saver.calls)
	}
}

func TestPersistAndResetBatchStopsAtEachFailedPhaseWithoutPublishing(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*testBatchSaver)
		wantCalls []string
	}{
		{
			name: "links",
			configure: func(saver *testBatchSaver) {
				saver.linkErr = errors.New("links unavailable")
			},
			wantCalls: []string{"pages", "links"},
		},
		{
			name: "images",
			configure: func(saver *testBatchSaver) {
				saver.imageErr = errors.New("images unavailable")
			},
			wantCalls: []string{"pages", "links", "images"},
		},
		{
			name: "publication",
			configure: func(saver *testBatchSaver) {
				saver.publishErr = errors.New("queue unavailable")
			},
			wantCalls: []string{"pages", "links", "images", "publish"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			page := pages.CreatePage("https://example.com/", "<html></html>", "text/html", 200)
			crawcfg := &crawler.CrawlerConfig{
				Mu:           &sync.Mutex{},
				Pages:        map[string]*pages.Page{page.NormalizedURL: page},
				Outlinks:     make(map[string]*pages.PageNode),
				Backlinks:    make(map[string]*pages.PageNode),
				Images:       make(map[string][]*pages.Image),
				Aliases:      map[string]int{page.NormalizedURL: 0},
				PageAttempts: 1,
			}
			saver := &testBatchSaver{}
			test.configure(saver)

			if err := persistAndResetBatch(crawcfg, saver, saver, saver, saver); err == nil {
				t.Fatal("persistAndResetBatch returned nil")
			}
			if !reflect.DeepEqual(saver.calls, test.wantCalls) {
				t.Fatalf("calls = %v, want %v", saver.calls, test.wantCalls)
			}
			if len(crawcfg.Pages) != 1 || len(crawcfg.Aliases) != 1 || crawcfg.PageAttempts != 1 {
				t.Fatalf("failed batch was cleared: %#v", crawcfg)
			}
		})
	}
}

func TestPersistAndResetBatchPublishesBeforeReturningBatchError(t *testing.T) {
	batchErr := errors.New("enqueue failed")
	page := pages.CreatePage("https://example.com/", "<html></html>", "text/html", 200)
	crawcfg := &crawler.CrawlerConfig{
		Mu:           &sync.Mutex{},
		Pages:        map[string]*pages.Page{page.NormalizedURL: page},
		Outlinks:     make(map[string]*pages.PageNode),
		Backlinks:    make(map[string]*pages.PageNode),
		Images:       make(map[string][]*pages.Image),
		Aliases:      map[string]int{page.NormalizedURL: 0},
		PageAttempts: 1,
		BatchError:   batchErr,
	}
	saver := &testBatchSaver{}

	err := persistAndResetBatch(crawcfg, saver, saver, saver, saver)
	if !errors.Is(err, batchErr) {
		t.Fatalf("error = %v, want batch error", err)
	}
	if !reflect.DeepEqual(saver.calls, []string{"pages", "links", "images", "publish"}) {
		t.Fatalf("batch-error calls = %v", saver.calls)
	}
	if !saver.published {
		t.Fatal("persisted page was not published")
	}
	if len(crawcfg.Pages) != 1 || crawcfg.BatchError != batchErr || crawcfg.PageAttempts != 1 {
		t.Fatalf("batch-error state was reset: %#v", crawcfg)
	}
}

func TestPersistAndResetBatchDefersBatchErrorUntilPersistenceSucceeds(t *testing.T) {
	batchErr := errors.New("enqueue failed")
	persistenceErr := errors.New("redis unavailable")
	crawcfg := &crawler.CrawlerConfig{
		Mu:         &sync.Mutex{},
		Pages:      make(map[string]*pages.Page),
		Outlinks:   make(map[string]*pages.PageNode),
		Backlinks:  make(map[string]*pages.PageNode),
		Images:     make(map[string][]*pages.Image),
		Aliases:    make(map[string]int),
		BatchError: batchErr,
	}
	saver := &testBatchSaver{pageErr: persistenceErr}

	err := persistAndResetBatch(crawcfg, saver, saver, saver, saver)
	if errors.Is(err, batchErr) || !errors.Is(err, persistenceErr) {
		t.Fatalf("persistence error = %v", err)
	}
	if !reflect.DeepEqual(saver.calls, []string{"pages"}) {
		t.Fatalf("persistence-error calls = %v", saver.calls)
	}
}

func TestPersistAndResetBatchRetriesPublicationBeforeReturningBatchError(t *testing.T) {
	batchErr := errors.New("enqueue failed")
	page := pages.CreatePage("https://example.com/", "<html></html>", "text/html", 200)
	crawcfg := &crawler.CrawlerConfig{
		Pages:      map[string]*pages.Page{page.NormalizedURL: page},
		Outlinks:   make(map[string]*pages.PageNode),
		Backlinks:  make(map[string]*pages.PageNode),
		Images:     make(map[string][]*pages.Image),
		Aliases:    make(map[string]int),
		BatchError: batchErr,
	}
	saver := &transientBatchSaver{failures: map[string]int{"publish": 1}}
	var sleeps []time.Duration

	err := persistAndResetBatchWithRetry(crawcfg, saver, saver, saver, saver, func(delay time.Duration) {
		sleeps = append(sleeps, delay)
	})
	if !errors.Is(err, batchErr) {
		t.Fatalf("error = %v, want batch error", err)
	}
	if !saver.published {
		t.Fatal("persisted page was not published")
	}
	if !reflect.DeepEqual(saver.calls, []string{"pages", "links", "images", "publish", "pages", "links", "images", "publish"}) {
		t.Fatalf("calls = %v", saver.calls)
	}
	if !reflect.DeepEqual(sleeps, []time.Duration{minimumPersistenceBackoff}) {
		t.Fatalf("sleeps = %v", sleeps)
	}
}

func TestPersistAndResetBatchRetriesTransientFailuresWithoutClearingMemory(t *testing.T) {
	for _, test := range []struct {
		name                  string
		failures              map[string]int
		commitPublishFailures int
		wantCalls             []string
		wantSleep             []time.Duration
	}{
		{
			name:      "page persistence",
			failures:  map[string]int{"pages": 2},
			wantCalls: []string{"pages", "pages", "pages", "links", "images", "publish"},
			wantSleep: []time.Duration{minimumPersistenceBackoff, 2 * minimumPersistenceBackoff},
		},
		{
			name:      "publication",
			failures:  map[string]int{"publish": 1},
			wantCalls: []string{"pages", "links", "images", "publish", "pages", "links", "images", "publish"},
			wantSleep: []time.Duration{minimumPersistenceBackoff},
		},
		{
			name:                  "ambiguous publication acknowledgement",
			failures:              make(map[string]int),
			commitPublishFailures: 1,
			wantCalls:             []string{"pages", "links", "images", "publish"},
			wantSleep:             []time.Duration{minimumPersistenceBackoff},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			page := pages.CreatePage("https://example.com/", "<html></html>", "text/html", 200)
			crawcfg := &crawler.CrawlerConfig{
				Pages:        map[string]*pages.Page{page.NormalizedURL: page},
				Outlinks:     make(map[string]*pages.PageNode),
				Backlinks:    make(map[string]*pages.PageNode),
				Images:       make(map[string][]*pages.Image),
				Aliases:      map[string]int{page.NormalizedURL: 0},
				PageAttempts: 1,
			}
			saver := &transientBatchSaver{
				failures:              test.failures,
				commitPublishFailures: test.commitPublishFailures,
			}
			var sleeps []time.Duration

			err := persistAndResetBatchWithRetry(crawcfg, saver, saver, saver, saver, func(delay time.Duration) {
				if len(crawcfg.Pages) != 1 || crawcfg.PageAttempts != 1 {
					t.Fatalf("batch was cleared before retry: %#v", crawcfg)
				}
				sleeps = append(sleeps, delay)
			})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(saver.calls, test.wantCalls) {
				t.Fatalf("calls = %v, want %v", saver.calls, test.wantCalls)
			}
			if !reflect.DeepEqual(sleeps, test.wantSleep) {
				t.Fatalf("sleeps = %v, want %v", sleeps, test.wantSleep)
			}
			if len(crawcfg.Pages) != 0 || crawcfg.PageAttempts != 0 {
				t.Fatalf("successful batch was not cleared: %#v", crawcfg)
			}
		})
	}
}

func TestPersistenceRetryLogOmitsWrappedURLAndContent(t *testing.T) {
	page := pages.CreatePage("https://example.com/", "<html></html>", "text/html", 200)
	crawcfg := &crawler.CrawlerConfig{
		Pages:     map[string]*pages.Page{page.NormalizedURL: page},
		Outlinks:  make(map[string]*pages.PageNode),
		Backlinks: make(map[string]*pages.PageNode),
		Images:    make(map[string][]*pages.Image),
		Aliases:   make(map[string]int),
	}
	sensitive := "https://example.com/private?token=do-not-log <html>private</html>"
	saver := &transientBatchSaver{
		failures:   map[string]int{"pages": 1},
		failureErr: errors.New(sensitive),
	}

	var output bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previousOutput) })

	if err := persistAndResetBatchWithRetry(crawcfg, saver, saver, saver, saver, func(time.Duration) {}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), sensitive) || strings.Contains(output.String(), "do-not-log") {
		t.Fatalf("retry log leaked wrapped error: %s", output.String())
	}
	if !strings.Contains(output.String(), "Crawl batch persistence failed") {
		t.Fatalf("retry log lost its actionable category: %s", output.String())
	}
}

func TestBatchPhaseErrorRedactsMessageAndPreservesCause(t *testing.T) {
	cause := errors.New("https://example.com/private?token=do-not-log")
	err := wrapBatchPhase("page persistence", cause)
	if !errors.Is(err, cause) {
		t.Fatal("batch phase error does not preserve its cause")
	}
	if strings.Contains(err.Error(), "example.com") || !strings.Contains(err.Error(), "page persistence") {
		t.Fatalf("batch phase error is not safely categorized: %v", err)
	}
}

func TestPersistAndResetBatchDoesNotRetryCrawlBatchErrors(t *testing.T) {
	batchErr := errors.New("crawl queue failed")
	crawcfg := &crawler.CrawlerConfig{
		Pages:      make(map[string]*pages.Page),
		Outlinks:   make(map[string]*pages.PageNode),
		Backlinks:  make(map[string]*pages.PageNode),
		Images:     make(map[string][]*pages.Image),
		Aliases:    make(map[string]int),
		BatchError: batchErr,
	}
	saver := &testBatchSaver{}

	err := persistAndResetBatchWithRetry(crawcfg, saver, saver, saver, saver, func(time.Duration) {
		t.Fatal("crawl batch error was retried")
	})
	if !errors.Is(err, batchErr) {
		t.Fatalf("error = %v, want batch error", err)
	}
}

func TestPersistAndResetBatchDoesNotRetryInvalidPages(t *testing.T) {
	crawcfg := &crawler.CrawlerConfig{
		Pages:     make(map[string]*pages.Page),
		Outlinks:  make(map[string]*pages.PageNode),
		Backlinks: make(map[string]*pages.PageNode),
		Images:    make(map[string][]*pages.Image),
		Aliases:   make(map[string]int),
	}
	saver := &testBatchSaver{pageErr: fmt.Errorf("%w: missing provenance", controllers.ErrInvalidPage)}

	err := persistAndResetBatchWithRetry(crawcfg, saver, saver, saver, saver, func(time.Duration) {
		t.Fatal("invalid page was retried")
	})
	if !errors.Is(err, controllers.ErrInvalidPage) {
		t.Fatalf("error = %v, want invalid page error", err)
	}
}

func TestBaselinePolicyRequiresApprovedExactHash(t *testing.T) {
	policyPath := filepath.Join("..", "..", "config", "crawl-policy-v1.baseline.json")
	policyBytes, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := crawlpolicy.Decode(bytes.NewReader(policyBytes), utils.DefaultUserAgent)
	if err != nil {
		t.Fatal(err)
	}
	approvedHash := policySHA256(policyBytes)
	if approvedHash != approvedBaselinePolicySHA256 {
		t.Fatalf("tracked baseline hash = %s, want %s", approvedHash, approvedBaselinePolicySHA256)
	}
	if err := requireBaselinePolicyEnvelope(policy, approvedHash); err != nil {
		t.Fatalf("approved baseline rejected: %v", err)
	}

	modifiedBytes := bytes.Replace(policyBytes, []byte("archive.org"), []byte("example.org"), 1)
	if bytes.Equal(modifiedBytes, policyBytes) {
		t.Fatal("test substitution did not change policy")
	}
	modifiedPolicy, err := crawlpolicy.Decode(bytes.NewReader(modifiedBytes), utils.DefaultUserAgent)
	if err != nil {
		t.Fatalf("one-for-one host substitution should remain semantically valid: %v", err)
	}
	modifiedHash := policySHA256(modifiedBytes)
	err = requireBaselinePolicyEnvelope(modifiedPolicy, modifiedHash)
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("modified policy error = %v, want hash rejection", err)
	}
}

func TestProductionRobotsEnvelopeAlwaysFailsClosed(t *testing.T) {
	policyDocument := func(onError crawlpolicy.RobotsErrorAction) string {
		return fmt.Sprintf(`{
			"schema_version":1,
			"unmatched_action":"deny",
			"groups":[{
				"id":"test",
				"enabled":true,
				"priority":1,
				"host_rules":[{"host":"example.com","match":"exact"}],
				"allowed_schemes":["https"],
				"max_depth":1,
				"allow_path_prefixes":[],
				"deny_path_prefixes":[],
				"min_request_interval":"1s",
				"max_concurrency":1,
				"max_requests_per_batch":1,
				"redirects":{"mode":"none","max_hops":0},
				"robots":{"mode":"enforce","on_error":%q,"cache_ttl":"1h"}
			}]
		}`, onError)
	}

	denyPolicy, err := crawlpolicy.Decode(strings.NewReader(policyDocument(crawlpolicy.RobotsErrorDeny)), utils.DefaultUserAgent)
	if err != nil {
		t.Fatal(err)
	}
	if err := requireProductionRobotsEnvelope(denyPolicy); err != nil {
		t.Fatalf("fail-closed policy was rejected: %v", err)
	}

	allowPolicy, err := crawlpolicy.Decode(strings.NewReader(policyDocument(crawlpolicy.RobotsErrorAllow)), utils.DefaultUserAgent)
	if err != nil {
		t.Fatal(err)
	}
	if err := requireProductionRobotsEnvelope(allowPolicy); err == nil || !strings.Contains(err.Error(), "on_error=deny") {
		t.Fatalf("fail-open policy error = %v", err)
	}

	globalSpoofPolicy, err := crawlpolicy.Decode(strings.NewReader(policyDocument(crawlpolicy.RobotsErrorDeny)), "Googlebot")
	if err != nil {
		t.Fatal(err)
	}
	if err := requireProductionRobotsEnvelope(globalSpoofPolicy); err == nil || !strings.Contains(err.Error(), "user_agent=MiFolyoBot/1.0") {
		t.Fatalf("global user-agent spoof error = %v", err)
	}

	groupSpoofDocument := strings.Replace(
		policyDocument(crawlpolicy.RobotsErrorDeny),
		`"redirects"`,
		`"user_agent":"Googlebot","redirects"`,
		1,
	)
	groupSpoofPolicy, err := crawlpolicy.Decode(strings.NewReader(groupSpoofDocument), utils.DefaultUserAgent)
	if err != nil {
		t.Fatal(err)
	}
	if err := requireProductionRobotsEnvelope(groupSpoofPolicy); err == nil || !strings.Contains(err.Error(), "user_agent=MiFolyoBot/1.0") {
		t.Fatalf("group user-agent spoof error = %v", err)
	}
	if err := requireProductionRobotsEnvelope(nil); err == nil {
		t.Fatal("nil policy was accepted")
	}
}

func TestSupportedRenderPolicyAllowsBrokeredV2OutsideBaseline(t *testing.T) {
	document := `{
		"schema_version": 1,
		"default_action": "deny",
		"rules": [{
			"id": "brokered",
			"enabled": true,
			"host_rule": {"host": "render.example.org", "match": "exact"},
			"allow_paths": ["/app"],
			"allow_path_prefixes": [],
			"deny_path_prefixes": [],
			"mode": "brokered",
			"failure_action": "reject_page",
			"resource_rules": [{
				"host_rule": {"host": "cdn.example.org", "match": "exact"},
				"allow_paths": ["/app.js", "/app.css"],
				"allow_path_prefixes": [],
				"deny_path_prefixes": [],
				"allowed_types": ["script", "stylesheet"]
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
				"max_resource_requests": 2,
				"max_aggregate_resource_bytes": 1024,
				"max_resource_body_bytes": 512,
				"max_rendered_dom_bytes": 1024,
				"max_dom_nodes": 100,
				"max_redirect_hops": 0,
				"max_console_bytes": 0
			}
		}]
	}`
	policy, err := renderpolicy.Decode(strings.NewReader(document))
	if err != nil {
		t.Fatal(err)
	}
	if err := requireSupportedRenderPolicy(policy, false); err != nil {
		t.Fatalf("brokered V2 policy was rejected: %v", err)
	}
	if err := requireSupportedRenderPolicy(policy, true); err == nil {
		t.Fatal("baseline accepted an enabled brokered render rule")
	}
}

func TestBaselineExecutionEnvelopeIsBoundedAndOneShot(t *testing.T) {
	for _, test := range []struct {
		name           string
		validateOnly   bool
		once           bool
		maxConcurrency int
		maxPages       int
		startingURL    string
		userAgent      string
		crawlQueueKey  string
		crawlURLsKey   string
		crawlDepthsKey string
		wantError      bool
	}{
		{name: "validation only", validateOnly: true, maxConcurrency: 2, maxPages: 10},
		{name: "validation only ignores starting URL", validateOnly: true, maxConcurrency: 2, maxPages: 10, startingURL: "https://example.com/"},
		{name: "bounded crawl", once: true, maxConcurrency: 2, maxPages: 10},
		{name: "continuous crawl", maxConcurrency: 2, maxPages: 10, wantError: true},
		{name: "starting URL override", once: true, maxConcurrency: 2, maxPages: 10, startingURL: "https://example.com/", wantError: true},
		{name: "user agent override", once: true, maxConcurrency: 2, maxPages: 10, userAgent: "OtherBot/1.0", wantError: true},
		{name: "queue key override", once: true, maxConcurrency: 2, maxPages: 10, crawlQueueKey: "other", wantError: true},
		{name: "too much concurrency", once: true, maxConcurrency: 3, maxPages: 10, wantError: true},
		{name: "too many attempts", once: true, maxConcurrency: 2, maxPages: 11, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			userAgent := test.userAgent
			if userAgent == "" {
				userAgent = utils.DefaultUserAgent
			}
			crawlQueueKey := test.crawlQueueKey
			if crawlQueueKey == "" {
				crawlQueueKey = utils.CrawlQueueKeyV1
			}
			crawlURLsKey := test.crawlURLsKey
			if crawlURLsKey == "" {
				crawlURLsKey = utils.CrawlURLsKeyV1
			}
			crawlDepthsKey := test.crawlDepthsKey
			if crawlDepthsKey == "" {
				crawlDepthsKey = utils.CrawlDepthsKeyV1
			}
			err := requireBaselineExecutionEnvelope(
				test.validateOnly,
				test.once,
				test.maxConcurrency,
				test.maxPages,
				test.startingURL,
				userAgent,
				crawlQueueKey,
				crawlURLsKey,
				crawlDepthsKey,
			)
			if (err != nil) != test.wantError {
				t.Fatalf("requireBaselineExecutionEnvelope error = %v, wantError=%t", err, test.wantError)
			}
		})
	}
}

func TestNoWorkBackoffIsBounded(t *testing.T) {
	backoff := time.Duration(0)
	for range 20 {
		backoff = nextNoWorkBackoff(backoff)
	}
	if backoff != maximumNoWorkBackoff {
		t.Fatalf("backoff = %s, want %s", backoff, maximumNoWorkBackoff)
	}
}
