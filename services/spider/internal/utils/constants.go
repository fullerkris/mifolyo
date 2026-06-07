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

	// FIXME: There is a weird "bug" where pages_queue starts appearing in redis even if it is not used in the code.
	// No idea why :/ and I don't have time to investigate it now.
	// Message Queues
	SpiderQueueKey      = "spider_queue"
	IndexerQueueKey     = "pages_queue"
	SignalQueueKey      = "signal_queue"
	ResumeCrawl         = "RESUME_CRAWL"
	MaxIndexerQueueSize = 5000

	// Redis Data: some keys stay in Redis indefinitely, while others are transfer to MongoDB by other services
	NormalizedURLPrefix = "normalized_url" // Stays in Redis indefinitely
	PagePrefix          = "page_data"      // Transferred by the indexer
	ImagePrefix         = "image_data"     // Transferred by the image indexer
	PageImagesPrefix    = "page_images"    // Transferred by the image indexer
	BacklinksPrefix     = "backlinks"      // Transferred by the backlinks processor
	OutlinksPrefix      = "outlinks"       // Transferred by the indexer
)
