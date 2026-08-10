package crawler

import (
	"errors"
	"log"
	"math"

	"github.com/IonelPopJara/search-engine/services/spider/internal/database"
	"github.com/IonelPopJara/search-engine/services/spider/internal/pages"
	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

// BFS crawling
func (crawcfg *CrawlerConfig) Crawl(db *database.Database) {
	// Starting a new webcrawler instance
	defer crawcfg.Wg.Done()

	// BFS loop
	for {
		log.Printf("Crawling...\n")
		// Reserve the batch budget before touching the queue. This makes
		// --max-pages a hard upper bound even when workers fail or overlap.
		if !crawcfg.reservePageAttempt() {
			log.Printf("Maximum number of page attempts reached\n")
			return
		}

		// Get the next URL from the queue
		log.Printf("Waiting for message queue...\n")
		currentURLID, canonicalCurrentURL, depthLevel, err := db.PopURL()
		if err != nil {
			if errors.Is(err, database.ErrOrphanURLLookup) ||
				errors.Is(err, database.ErrInvalidQueueEntry) ||
				errors.Is(err, database.ErrInvalidURLID) ||
				errors.Is(err, database.ErrInvalidQueueMember) {
				log.Printf("Discarded invalid queue entry %s: %v\n", currentURLID, err)
				continue
			}
			log.Printf("No more URLs in the queue: %v\n", err)
			return
		}

		log.Printf("Popped URL ID: %v | Depth Level: %v | Canonical URL: %v\n", currentURLID, depthLevel, canonicalCurrentURL)

		// time.Sleep(1 * time.Second)

		// Check if the URL has been visited
		visited, err := db.HasURLBeenVisited(canonicalCurrentURL)
		if err != nil {
			log.Printf("Error: [%v] - skipping...\n", err)
			continue
		}

		if visited {
			log.Printf("Skipping %v - already visited\n", canonicalCurrentURL)
			continue
		}

		log.Printf("Crawling %v...\n", canonicalCurrentURL)

		// Fetch HTML, Status Code, and Content-Type
		html, statusCode, contentType, err := getPageData(canonicalCurrentURL, crawcfg.UserAgent)
		if err != nil {
			// Skip if we couldn't fetch the data
			log.Printf("Error fetching %v data: %v\n", canonicalCurrentURL, err)
			continue
		}

		// Fetch the links of the current page
		outgoingLinks, imagesMap, err := getURLsFromHTML(html, canonicalCurrentURL)
		if err != nil {
			log.Printf("Error getting links from HTML: %v\n", err)
			continue
		}

		// Store images
		crawcfg.AddImages(canonicalCurrentURL, imagesMap)

		// Create outlinks and update backlinks
		crawcfg.UpdateLinks(canonicalCurrentURL, outgoingLinks)

		// Create Page struct
		pg := pages.CreatePage(canonicalCurrentURL, html, contentType, statusCode)

		// Add page visit
		err = crawcfg.addPage(pg)
		if err != nil {
			log.Printf("\tError adding page visit: %v\n", err)
			continue
		}

		err = db.VisitPage(canonicalCurrentURL)
		if err != nil {
			log.Printf("\tError adding page visit: %v\n", err)
			continue
		}

		log.Printf("Adding links from %v...\n", canonicalCurrentURL)
		// Add links to url queue
		for _, outgoingLink := range outgoingLinks {
			identity, err := utils.CanonicalizeURLV1(outgoingLink)
			if err != nil || !identity.CrawlEligible || identity.CanonicalURL == canonicalCurrentURL {
				continue
			}

			// Check if the thing exists in the queue, and update weight
			score, exists, err := db.ExistsInQueue(identity.URLID)
			if err != nil {
				log.Printf("Error checking queue for %s: %v\n", identity.CanonicalURL, err)
				continue
			}
			if exists {
				// NOTE: I decided to disable this for now.
				// I'll see how it performs without it.
				// score -= 0.001
			} else {
				score = depthLevel + 1
			}

			score = math.Max(utils.MinScore, math.Min(score, utils.MaxScore))

			// Update score based on depth
			if err := db.PushURL(identity.CanonicalURL, score); err != nil {
				log.Printf("Error queueing %s: %v\n", identity.CanonicalURL, err)
			}
		}
	}
}
