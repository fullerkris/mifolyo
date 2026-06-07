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
	MaxPages       int    // Max discovered pages
	MaxConcurrency int    // Maximum concurrent workers in the pool
	UserAgent      string // User agent sent with HTTP crawl requests
}

func (crawcfg *CrawlerConfig) lenPages() int {
	crawcfg.Mu.Lock()
	defer crawcfg.Mu.Unlock()

	return len(crawcfg.Pages)
}

func (crawcfg *CrawlerConfig) maxPagesReached() bool {
	crawcfg.Mu.Lock()
	defer crawcfg.Mu.Unlock()

	if len(crawcfg.Pages) >= crawcfg.MaxPages {
		// Can't add more pages because max pages has been reached
		return true
	}

	// Max pages has not been reached
	return false
}

func (crawcfg *CrawlerConfig) addPage(page *pages.Page) error {
	crawcfg.Mu.Lock()
	defer crawcfg.Mu.Unlock()

	normalizedURL := page.NormalizedURL

	if _, visited := crawcfg.Pages[normalizedURL]; visited {
		return fmt.Errorf("Page already visited")
	}

	if len(crawcfg.Pages) >= crawcfg.MaxPages {
		// Can't add more pages because max pages has been reached
		return fmt.Errorf("Max pages reached")
	}

	crawcfg.Pages[normalizedURL] = page
	return nil
}

func (crawcfg *CrawlerConfig) UpdateLinks(normalizedCurrentURL string, outgoingLinks []string) {
	crawcfg.Mu.Lock()
	defer crawcfg.Mu.Unlock()

	crawcfg.Outlinks[normalizedCurrentURL] = pages.CreatePageNode(normalizedCurrentURL)
	for _, link := range outgoingLinks {
		if utils.IsValidURL(link) {
			// normalize url
			normalizedOutgoingURL, err := utils.NormalizeURL(link)
			if err != nil {
				continue
			}

			if normalizedOutgoingURL == normalizedCurrentURL {
				continue
			}

			// If the entry does not exist
			if _, exists := crawcfg.Backlinks[normalizedOutgoingURL]; !exists {
				crawcfg.Backlinks[normalizedOutgoingURL] = pages.CreatePageNode(normalizedOutgoingURL)
			}

			crawcfg.Backlinks[normalizedOutgoingURL].AppendLink(normalizedCurrentURL)
			crawcfg.Outlinks[normalizedCurrentURL].AppendLink(normalizedOutgoingURL)
		}
	}
}

func (crawcfg *CrawlerConfig) AddImages(normalizedCurrentURL string, imagesMap map[string]map[string]string) {
	crawcfg.Mu.Lock()
	defer crawcfg.Mu.Unlock()

	for imgURL, imgAttrs := range imagesMap {
		imgAlt := ""
		if alt, exists := imgAttrs["alt"]; exists {
			imgAlt = alt
		}

		image := &pages.Image{
			NormalizedPageURL:   normalizedCurrentURL,
			NormalizedSourceURL: imgURL,
			Alt:                 imgAlt,
		}

		crawcfg.Images[normalizedCurrentURL] = append(crawcfg.Images[normalizedCurrentURL], image)
	}
}
