package crawler

import (
	"fmt"
	"sync"

	"github.com/IonelPopJara/search-engine/services/spider/internal/pages"
	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

// When the pages reaches a length of maxPages, stop the cycle, fetch/write data, and start again
type CrawlerConfig struct {
	Mu             *sync.Mutex                // Sync
	Wg             *sync.WaitGroup            // Sync
	Pages          map[string]*pages.Page     // Discovered pages
	Outlinks       map[string]*pages.PageNode // Discovered outlinks
	Backlinks      map[string]*pages.PageNode // Discovered backlinks
	Images         map[string][]*pages.Image
	MaxPages       int    // Maximum page attempts in one batch
	PageAttempts   int    // Page attempts reserved in the current batch
	MaxConcurrency int    // Maximum concurrent workers in the pool
	UserAgent      string // User agent sent with HTTP crawl requests
}

func (crawcfg *CrawlerConfig) lenPages() int {
	crawcfg.Mu.Lock()
	defer crawcfg.Mu.Unlock()

	return len(crawcfg.Pages)
}

func (crawcfg *CrawlerConfig) reservePageAttempt() bool {
	crawcfg.Mu.Lock()
	defer crawcfg.Mu.Unlock()

	if crawcfg.PageAttempts >= crawcfg.MaxPages {
		return false
	}

	crawcfg.PageAttempts++
	return true
}

func (crawcfg *CrawlerConfig) addPage(page *pages.Page) error {
	crawcfg.Mu.Lock()
	defer crawcfg.Mu.Unlock()

	canonicalURL := page.NormalizedURL

	if _, visited := crawcfg.Pages[canonicalURL]; visited {
		return fmt.Errorf("Page already visited")
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

func (crawcfg *CrawlerConfig) AddImages(canonicalCurrentURL string, imagesMap map[string]map[string]string) {
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
