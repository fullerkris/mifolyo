package crawler

import (
	"context"
	"fmt"
	"sync"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawlpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/pages"
	"github.com/IonelPopJara/search-engine/services/spider/internal/renderclient"
	"github.com/IonelPopJara/search-engine/services/spider/internal/renderpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/robotsguard"
	"github.com/IonelPopJara/search-engine/services/spider/internal/securefetch"
	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

// RobotsAuthorizer is the narrow robots decision boundary used by the crawler.
// Production wiring supplies *robotsguard.Manager; tests may provide hermetic
// fixtures without adding a runtime robots bypass.
type RobotsAuthorizer interface {
	Allowed(context.Context, crawlpolicy.Decision, securefetch.RequestGate) (bool, error)
}

var _ RobotsAuthorizer = (*robotsguard.Manager)(nil)

// When the pages reaches a length of maxPages, stop the cycle, fetch/write data, and start again
type CrawlerConfig struct {
	Mu                 *sync.Mutex                // Sync
	Wg                 *sync.WaitGroup            // Sync
	Pages              map[string]*pages.Page     // Discovered pages
	Outlinks           map[string]*pages.PageNode // Discovered outlinks
	Backlinks          map[string]*pages.PageNode // Discovered backlinks
	Images             map[string][]*pages.Image
	Aliases            map[string]int // Lowest depth at which an alias was fully processed.
	MaxPages           int            // Maximum page attempts in one batch
	PageAttempts       int            // Outbound request capacity reserved or used in this batch
	MaxConcurrency     int            // Maximum concurrent workers in the pool
	UserAgent          string         // User agent sent with HTTP crawl requests
	Policy             *crawlpolicy.Policy
	PolicyRuntime      *crawlpolicy.Runtime
	Fetcher            *securefetch.Fetcher
	Robots             RobotsAuthorizer
	RenderPolicy       *renderpolicy.Policy
	RenderPolicySHA256 string
	Renderer           renderclient.Renderer
	BatchError         error
	scheduleMu         sync.Mutex
	renderMu           sync.Mutex
}

// requestBudgetReservation owns one global request slot until it is either
// committed to a RequestGate Acquire or refunded before any request attempt.
type requestBudgetReservation struct {
	crawler *CrawlerConfig
	once    sync.Once
}

func (reservation *requestBudgetReservation) commit() {
	if reservation == nil {
		return
	}
	reservation.once.Do(func() {})
}

func (reservation *requestBudgetReservation) refund() {
	if reservation == nil {
		return
	}
	reservation.once.Do(func() {
		reservation.crawler.Mu.Lock()
		if reservation.crawler.PageAttempts > 0 {
			reservation.crawler.PageAttempts--
		}
		reservation.crawler.Mu.Unlock()
	})
}

func (crawcfg *CrawlerConfig) recordBatchError(err error) {
	if err == nil {
		return
	}
	crawcfg.Mu.Lock()
	if crawcfg.BatchError == nil {
		crawcfg.BatchError = err
	}
	crawcfg.Mu.Unlock()
}

func (crawcfg *CrawlerConfig) processedAtSameOrShallowerDepth(canonicalURL string, depth int) bool {
	crawcfg.Mu.Lock()
	defer crawcfg.Mu.Unlock()
	processedDepth, exists := crawcfg.Aliases[canonicalURL]
	return exists && processedDepth <= depth
}

func (crawcfg *CrawlerConfig) recordAliasesAtDepth(depth int, canonicalURLs ...string) {
	crawcfg.Mu.Lock()
	defer crawcfg.Mu.Unlock()
	if crawcfg.Aliases == nil {
		crawcfg.Aliases = make(map[string]int)
	}
	for _, canonicalURL := range canonicalURLs {
		processedDepth, exists := crawcfg.Aliases[canonicalURL]
		if !exists || depth < processedDepth {
			crawcfg.Aliases[canonicalURL] = depth
		}
	}
}

func (crawcfg *CrawlerConfig) lenPages() int {
	crawcfg.Mu.Lock()
	defer crawcfg.Mu.Unlock()

	return len(crawcfg.Pages)
}

func (crawcfg *CrawlerConfig) reservePageAttempt() (*requestBudgetReservation, bool) {
	crawcfg.Mu.Lock()
	defer crawcfg.Mu.Unlock()

	if crawcfg.PageAttempts >= crawcfg.MaxPages {
		return nil, false
	}

	crawcfg.PageAttempts++
	return &requestBudgetReservation{crawler: crawcfg}, true
}

func (crawcfg *CrawlerConfig) addPage(page *pages.Page) error {
	crawcfg.Mu.Lock()
	defer crawcfg.Mu.Unlock()

	canonicalURL := page.NormalizedURL

	if _, visited := crawcfg.Pages[canonicalURL]; visited {
		// Redirect aliases may need their outlinks propagated again at a
		// newly discovered shallower depth. Page storage itself is idempotent.
		return nil
	}

	if len(crawcfg.Pages) >= crawcfg.MaxPages {
		// Can't add more pages because max pages has been reached
		return fmt.Errorf("Max pages reached")
	}

	crawcfg.Pages[canonicalURL] = page
	return nil
}

func (crawcfg *CrawlerConfig) UpdateLinks(canonicalCurrentURL string, outgoingLinks []string) {
	currentIdentity, err := utils.CanonicalizeURLV1(canonicalCurrentURL)
	if err != nil {
		return
	}
	canonicalCurrentURL = currentIdentity.CanonicalURL

	crawcfg.Mu.Lock()
	defer crawcfg.Mu.Unlock()

	crawcfg.Outlinks[canonicalCurrentURL] = pages.CreatePageNode(canonicalCurrentURL)
	for _, link := range outgoingLinks {
		outgoingIdentity, err := utils.CanonicalizeURLV1(link)
		if err != nil {
			continue
		}
		canonicalOutgoingURL := outgoingIdentity.CanonicalURL

		if canonicalOutgoingURL == canonicalCurrentURL {
			continue
		}

		if _, exists := crawcfg.Backlinks[canonicalOutgoingURL]; !exists {
			crawcfg.Backlinks[canonicalOutgoingURL] = pages.CreatePageNode(canonicalOutgoingURL)
		}

		crawcfg.Backlinks[canonicalOutgoingURL].AppendLink(canonicalCurrentURL)
		crawcfg.Outlinks[canonicalCurrentURL].AppendLink(canonicalOutgoingURL)
	}
}

func (crawcfg *CrawlerConfig) AddImages(canonicalCurrentURL string, imagesMap map[string]map[string]string, depth int) {
	currentIdentity, err := utils.CanonicalizeURLV1(canonicalCurrentURL)
	if err != nil {
		return
	}
	canonicalCurrentURL = currentIdentity.CanonicalURL

	crawcfg.Mu.Lock()
	defer crawcfg.Mu.Unlock()

	for imgURL, imgAttrs := range imagesMap {
		imageIdentity, err := utils.CanonicalizeURLV1(imgURL)
		if err != nil {
			continue
		}
		if err := utils.RequireStaticCrawlEligibility(imageIdentity); err != nil || crawcfg.Policy == nil {
			continue
		}
		if _, err := crawcfg.Policy.Match(imageIdentity.CanonicalURL, depth); err != nil {
			continue
		}
		imgAlt := ""
		if alt, exists := imgAttrs["alt"]; exists {
			imgAlt = alt
		}

		image := &pages.Image{
			NormalizedPageURL:   canonicalCurrentURL,
			NormalizedSourceURL: imageIdentity.CanonicalURL,
			Alt:                 imgAlt,
		}

		crawcfg.Images[canonicalCurrentURL] = append(crawcfg.Images[canonicalCurrentURL], image)
	}
}
