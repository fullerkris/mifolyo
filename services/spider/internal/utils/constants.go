package utils

import (
	"time"
)

const (
	// Crawler constants
	DefaultUserAgent = "MiFolyoBot/1.0"
	Timeout          = 5 * time.Second
	MaxScore         = 10000
	MinScore         = -1000
	// MaxCrawlDepthV1 is the largest integer represented exactly by Redis Lua.
	MaxCrawlDepthV1 uint64 = 1<<53 - 1

	// FIXME: There is a weird "bug" where pages_queue starts appearing in redis even if it is not used in the code.
	// No idea why :/ and I don't have time to investigate it now.
	// Message Queues
	CrawlQueueKeyV1       = "mifolyo:crawl:v1:queue"
	CrawlURLsKeyV1        = "mifolyo:crawl:v1:urls"
	CrawlDepthsKeyV1      = "mifolyo:crawl:v1:depths"
	SpiderQueueKey        = CrawlQueueKeyV1 // Deprecated compatibility alias.
	IndexerQueueKey       = "pages_queue"
	PagePublicationPrefix = "page_publication"
	PagePublicationTTL    = 7 * 24 * time.Hour
	SignalQueueKey        = "signal_queue"
	ResumeCrawl           = "RESUME_CRAWL"
	MaxIndexerQueueSize   = 5000

	// Redis Data: some keys stay in Redis indefinitely, while others are transfer to MongoDB by other services
	NormalizedURLPrefix = "normalized_url" // Stays in Redis indefinitely
	PagePrefix          = "page_data"      // Transferred by the indexer
	ImagePrefix         = "image_data"     // Transferred by the image indexer
	// PageImagesPrefix identifies immutable image manifest hashes. Manifest and
	// payload keys are publication-scoped and have no TTL; the image indexer
	// removes only the exact keys after its MongoDB reconciliation is ACKed.
	PageImagesPrefix = "page_images"
	BacklinksPrefix  = "backlinks" // Transferred by the backlinks processor
	OutlinksPrefix   = "outlinks"  // Transferred by the indexer
)
