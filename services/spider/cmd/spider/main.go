package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/IonelPopJara/search-engine/services/spider/internal/controllers"
	"github.com/IonelPopJara/search-engine/services/spider/internal/crawler"
	"github.com/IonelPopJara/search-engine/services/spider/internal/crawlpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/database"
	"github.com/IonelPopJara/search-engine/services/spider/internal/pages"
	"github.com/IonelPopJara/search-engine/services/spider/internal/renderclient"
	"github.com/IonelPopJara/search-engine/services/spider/internal/renderpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/robotsguard"
	"github.com/IonelPopJara/search-engine/services/spider/internal/securefetch"
	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

const (
	approvedBaselinePolicySHA256 = "50648954d0264f7ac4fdda174178db488e86e335a0b63fdcc448da7bc218bae3"
	baselineMaxConcurrency       = 2
	baselineMaxPageAttempts      = 10
	minimumNoWorkBackoff         = 100 * time.Millisecond
	maximumNoWorkBackoff         = 2 * time.Second
	minimumPersistenceBackoff    = 100 * time.Millisecond
	maximumPersistenceBackoff    = 2 * time.Second
)

type pageBatchSaver interface {
	SavePages(*crawler.CrawlerConfig) error
}

type pageBatchPublisher interface {
	BatchPublished(*crawler.CrawlerConfig) (bool, error)
	PublishPages(*crawler.CrawlerConfig) error
}

type linkBatchSaver interface {
	SaveLinks(*crawler.CrawlerConfig) error
}

type imageBatchSaver interface {
	SaveImages(*crawler.CrawlerConfig) error
}

type batchPhaseError struct {
	phase string
	cause error
}

func (err *batchPhaseError) Error() string {
	return "crawl batch " + err.phase + " failed"
}

func (err *batchPhaseError) Unwrap() error {
	return err.cause
}

func wrapBatchPhase(phase string, cause error) error {
	return &batchPhaseError{phase: phase, cause: cause}
}

// getEnv retrieves the value of an environment variable or returns a fallback value if not set.
func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return fallback
}

func main() {
	if err := run(); err != nil {
		log.Printf("Spider failed category=crawl_execution\n")
		os.Exit(1)
	}
}

func run() error {
	// Parse flags
	maxConcurrency := flag.Int("max-concurrency", baselineMaxConcurrency, "Maximum number of concurrent workers")
	maxPages := flag.Int("max-pages", baselineMaxPageAttempts, "Maximum number of outbound request attempts per batch")
	once := flag.Bool("once", false, "Exit after one crawl batch")
	policyFile := flag.String("crawl-policy-file", getEnv("CRAWL_POLICY_FILE", ""), "Path to the required V1 domain-group crawl policy")
	renderPolicyFile := flag.String("render-policy-file", getEnv("RENDER_POLICY_FILE", ""), "Path to an optional V1 JavaScript render policy")
	renderSocket := flag.String("render-socket", getEnv("RENDER_SOCKET", ""), "Unix socket for the networkless render worker")
	validatePolicy := flag.Bool("validate-policy", false, "Validate and summarize the crawl policy without Redis or network access")
	validateBaselinePolicy := flag.Bool("validate-baseline-policy", false, "Require the bounded baseline safety envelope")
	flag.Parse()
	if *maxConcurrency < 1 || *maxPages < 1 {
		return fmt.Errorf("--max-concurrency and --max-pages must both be positive")
	}
	if *maxConcurrency > baselineMaxConcurrency || *maxPages > baselineMaxPageAttempts {
		return fmt.Errorf(
			"--max-concurrency must not exceed %d and --max-pages must not exceed %d",
			baselineMaxConcurrency,
			baselineMaxPageAttempts,
		)
	}

	// Retrieve environment variables
	redisHost := getEnv("REDIS_HOST", "localhost")
	redisPort := getEnv("REDIS_PORT", "6379")
	redisPassword := getEnv("REDIS_PASSWORD", "")
	redisDB := getEnv("REDIS_DB", "0")
	startingURL := getEnv("STARTING_URL", "")
	userAgent := getEnv("USER_AGENT", utils.DefaultUserAgent)
	crawlQueueKey := getEnv("CRAWL_QUEUE_KEY", utils.CrawlQueueKeyV1)
	crawlURLsKey := getEnv("CRAWL_URLS_KEY", utils.CrawlURLsKeyV1)
	crawlDepthsKey := getEnv("CRAWL_DEPTHS_KEY", utils.CrawlDepthsKeyV1)
	if strings.TrimSpace(*policyFile) == "" {
		return fmt.Errorf("CRAWL_POLICY_FILE or --crawl-policy-file is required")
	}

	policyBytes, err := os.ReadFile(*policyFile)
	if err != nil {
		return fmt.Errorf("read crawl policy: %w", err)
	}
	policy, err := crawlpolicy.Decode(bytes.NewReader(policyBytes), userAgent)
	if err != nil {
		return fmt.Errorf("decode crawl policy: %w", err)
	}
	if err := requireProductionRobotsEnvelope(policy); err != nil {
		return err
	}
	policyHash := policySHA256(policyBytes)
	var renderingPolicy *renderpolicy.Policy
	renderPolicyHash := ""
	if strings.TrimSpace(*renderPolicyFile) != "" {
		renderPolicyBytes, readErr := os.ReadFile(*renderPolicyFile)
		if readErr != nil {
			return fmt.Errorf("read render policy: %w", readErr)
		}
		renderingPolicy, err = renderpolicy.Decode(bytes.NewReader(renderPolicyBytes))
		if err != nil {
			return fmt.Errorf("decode render policy: %w", err)
		}
		renderPolicyHash = policySHA256(renderPolicyBytes)
		log.Printf(
			"Loaded render policy path=%s sha256=%s rules=%d enabled=%d\n",
			*renderPolicyFile,
			renderPolicyHash,
			len(renderingPolicy.Rules()),
			renderingPolicy.EnabledRuleCount(),
		)
	} else if strings.TrimSpace(*renderSocket) != "" {
		return fmt.Errorf("RENDER_POLICY_FILE or --render-policy-file is required when a render socket is configured")
	}
	if err := requireSupportedRenderPolicy(renderingPolicy, *validateBaselinePolicy); err != nil {
		return err
	}
	if *validateBaselinePolicy {
		if err := requireBaselinePolicyEnvelope(policy, policyHash); err != nil {
			return err
		}
		if err := requireBaselineExecutionEnvelope(
			*validatePolicy,
			*once,
			*maxConcurrency,
			*maxPages,
			startingURL,
			userAgent,
			crawlQueueKey,
			crawlURLsKey,
			crawlDepthsKey,
		); err != nil {
			return err
		}
	}
	log.Printf("Loaded crawl policy path=%s sha256=%s groups=%d\n", *policyFile, policyHash, len(policy.Groups()))
	if *validatePolicy {
		for _, group := range policy.Groups() {
			log.Printf(
				"Policy group id=%s enabled=%t priority=%d hosts=%d depth=%d interval=%s concurrency=%d batch_requests=%d redirects=%s/%d robots=%s\n",
				group.ID,
				group.Enabled,
				group.Priority,
				len(group.HostRules),
				group.MaxDepth,
				group.MinRequestInterval,
				group.MaxConcurrency,
				group.MaxRequestsPerBatch,
				group.Redirects.Mode,
				group.Redirects.MaxHops,
				group.Robots.OnError,
			)
		}
		return nil
	}
	var renderer renderclient.Renderer
	if renderingPolicy != nil && renderingPolicy.EnabledRuleCount() > 0 {
		if strings.TrimSpace(*renderSocket) == "" {
			return fmt.Errorf("RENDER_SOCKET or --render-socket is required by enabled render rules")
		}
		renderClient, clientErr := renderclient.New(*renderSocket)
		if clientErr != nil {
			return fmt.Errorf("initialize render worker client: %w", clientErr)
		}
		renderer = renderClient
	}
	deniedCIDRs, err := splitOptionalList(getEnv("CRAWL_DENY_CIDRS", ""))
	if err != nil {
		return fmt.Errorf("invalid CRAWL_DENY_CIDRS: %w", err)
	}
	fetcher, err := securefetch.New(securefetch.Config{AdditionalDeniedCIDRs: deniedCIDRs})
	if err != nil {
		return fmt.Errorf("initialize secure HTTP transport: %w", err)
	}
	policyRuntime := policy.NewRuntime()
	robots := robotsguard.New(policy, fetcher)

	// Connect to Redis
	db := &database.Database{
		CrawlQueueKey:  crawlQueueKey,
		CrawlURLsKey:   crawlURLsKey,
		CrawlDepthsKey: crawlDepthsKey,
	}
	err = db.ConnectToRedis(redisHost, redisPort, redisPassword, redisDB)
	if err != nil {
		return err
	}

	// STARTING_URL is an explicit development override. Production/default
	// startup consumes the versioned queue populated by the crawl feeder.
	if startingURL != "" {
		decision, matchErr := policy.Match(startingURL, 0)
		if matchErr != nil {
			return fmt.Errorf("STARTING_URL is denied by crawl policy: %w", matchErr)
		}
		if err := db.PushURLWithDepth(decision.Identity.CanonicalURL, 0, 0); err != nil {
			return fmt.Errorf("enqueue STARTING_URL: %w", err)
		}
		log.Printf("Queued STARTING_URL ID %s in group %s\n", decision.Identity.URLID, decision.Group.ID)
	}

	// Instantiate controllers
	pageController := controllers.NewPageController(db)
	linksController := controllers.NewLinksController(db)
	imageController := controllers.NewImageController(db)

	// Instantiate crawler
	crawler := &crawler.CrawlerConfig{
		Mu:                 &sync.Mutex{},
		Wg:                 &sync.WaitGroup{},
		Pages:              make(map[string]*pages.Page),
		Outlinks:           make(map[string]*pages.PageNode),
		Backlinks:          make(map[string]*pages.PageNode),
		Images:             make(map[string][]*pages.Image),
		Aliases:            make(map[string]int),
		MaxPages:           *maxPages,
		MaxConcurrency:     *maxConcurrency,
		UserAgent:          userAgent,
		Policy:             policy,
		PolicyRuntime:      policyRuntime,
		Fetcher:            fetcher,
		Robots:             robots,
		RenderPolicy:       renderingPolicy,
		RenderPolicySHA256: renderPolicyHash,
		Renderer:           renderer,
	}

	noWorkBackoff := time.Duration(0)
	// Infinite loop to crawl the web in batches
	for {
		// Check how busy the indexer queue is
		log.Printf("Checking number of entries...\n")
		// If we have reached the maximum number of entries in the spider queue
		queueSize, err := db.GetIndexerQueueSize()
		if err != nil {
			return fmt.Errorf("get indexer queue size: %w", err)
		}

		if queueSize >= utils.MaxIndexerQueueSize {
			log.Printf("Indexer queue is full. Waiting...\n")
			// Wait until we receive a signal to start crawling again
			for {
				sig, err := db.PopSignalQueue()
				if err != nil {
					return fmt.Errorf("get crawl resume signal: %w", err)
				}

				if sig == utils.ResumeCrawl {
					log.Printf("Resume crawl!\n")
					break
				}
			}
		}

		log.Printf("Spawning workers...\n")
		for range crawler.MaxConcurrency {
			crawler.Wg.Add(1)
			go crawler.Crawl(db)
		}

		crawler.Wg.Wait()
		attempts := crawler.PageAttempts
		pagesFetched := len(crawler.Pages)

		if err := persistAndResetBatchWithRetry(
			crawler,
			pageController,
			linksController,
			imageController,
			pageController,
			time.Sleep,
		); err != nil {
			return fmt.Errorf("complete crawl batch: %w", err)
		}
		log.Printf("Crawl batch summary outbound_budget_used=%d pages_fetched=%d\n", attempts, pagesFetched)

		if *once {
			log.Printf("One crawl batch completed. Exiting...\n")
			return nil
		}
		if attempts == 0 {
			noWorkBackoff = nextNoWorkBackoff(noWorkBackoff)
			log.Printf("No outbound work attempted; waiting %s before the next batch\n", noWorkBackoff)
			time.Sleep(noWorkBackoff)
		} else {
			noWorkBackoff = 0
		}
	}
}

func persistAndResetBatchWithRetry(
	crawcfg *crawler.CrawlerConfig,
	pageSaver pageBatchSaver,
	linkSaver linkBatchSaver,
	imageSaver imageBatchSaver,
	pagePublisher pageBatchPublisher,
	sleep func(time.Duration),
) error {
	backoff := time.Duration(0)
	for {
		err := persistAndResetBatch(crawcfg, pageSaver, linkSaver, imageSaver, pagePublisher)
		if err == nil {
			return nil
		}
		if (crawcfg.BatchError != nil && errors.Is(err, crawcfg.BatchError)) || errors.Is(err, controllers.ErrInvalidPage) {
			return err
		}

		backoff = nextPersistenceBackoff(backoff)
		log.Printf("Crawl batch persistence failed; retrying in %s\n", backoff)
		sleep(backoff)
	}
}

func persistAndResetBatch(
	crawcfg *crawler.CrawlerConfig,
	pageSaver pageBatchSaver,
	linkSaver linkBatchSaver,
	imageSaver imageBatchSaver,
	pagePublisher pageBatchPublisher,
) error {
	batchError := crawcfg.BatchError
	published, err := pagePublisher.BatchPublished(crawcfg)
	if err != nil {
		return wrapBatchPhase("publication check", err)
	}
	if published {
		if batchError != nil {
			return batchError
		}
		resetBatch(crawcfg)
		return nil
	}

	var persistenceError error
	if err := pageSaver.SavePages(crawcfg); err != nil {
		persistenceError = wrapBatchPhase("page persistence", err)
	} else if err := linkSaver.SaveLinks(crawcfg); err != nil {
		persistenceError = wrapBatchPhase("link persistence", err)
	} else if err := imageSaver.SaveImages(crawcfg); err != nil {
		persistenceError = wrapBatchPhase("image persistence", err)
	}
	if persistenceError != nil {
		return persistenceError
	}
	if err := pagePublisher.PublishPages(crawcfg); err != nil {
		return wrapBatchPhase("publication", err)
	}
	if batchError != nil {
		return batchError
	}

	resetBatch(crawcfg)
	return nil
}

func resetBatch(crawcfg *crawler.CrawlerConfig) {
	crawcfg.Pages = make(map[string]*pages.Page)
	crawcfg.Outlinks = make(map[string]*pages.PageNode)
	crawcfg.Backlinks = make(map[string]*pages.PageNode)
	crawcfg.Images = make(map[string][]*pages.Image)
	crawcfg.Aliases = make(map[string]int)
	crawcfg.PageAttempts = 0
	crawcfg.BatchError = nil
	crawcfg.PolicyRuntime.ResetBatch()
}

func nextNoWorkBackoff(current time.Duration) time.Duration {
	if current < minimumNoWorkBackoff {
		return minimumNoWorkBackoff
	}
	if current >= maximumNoWorkBackoff/2 {
		return maximumNoWorkBackoff
	}
	return current * 2
}

func nextPersistenceBackoff(current time.Duration) time.Duration {
	if current < minimumPersistenceBackoff {
		return minimumPersistenceBackoff
	}
	if current >= maximumPersistenceBackoff/2 {
		return maximumPersistenceBackoff
	}
	return current * 2
}

func policySHA256(document []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(document))
}

func requireProductionRobotsEnvelope(policy *crawlpolicy.Policy) error {
	if policy == nil {
		return fmt.Errorf("crawl policy is required")
	}
	for _, group := range policy.Groups() {
		if group.Robots.Mode != crawlpolicy.RobotsEnforce || group.Robots.OnError != crawlpolicy.RobotsErrorDeny {
			return fmt.Errorf("crawl policy group %s must enforce robots with on_error=deny", group.ID)
		}
		if group.UserAgent != utils.DefaultUserAgent {
			return fmt.Errorf("crawl policy group %s must use user_agent=%s", group.ID, utils.DefaultUserAgent)
		}
	}
	return nil
}

func requireBaselinePolicyEnvelope(policy *crawlpolicy.Policy, policyHash string) error {
	if policy == nil || policy.UnmatchedAction() != crawlpolicy.UnmatchedDeny {
		return fmt.Errorf("baseline policy must deny unmatched domains")
	}
	enabledGroups := 0
	enabledHostRules := 0
	groups := policy.Groups()
	if len(groups) == 0 {
		return fmt.Errorf("baseline policy must contain enabled groups")
	}
	for _, group := range groups {
		if len(group.AllowedSchemes) != 1 || group.AllowedSchemes[0] != "https" {
			return fmt.Errorf("baseline group %s must be HTTPS-only", group.ID)
		}
		if group.MaxDepth > 1 || group.MinRequestInterval < time.Second || group.MaxConcurrency > 1 || group.MaxRequestsPerBatch > 4 {
			return fmt.Errorf("baseline group %s exceeds the bounded crawl envelope", group.ID)
		}
		if group.Redirects.MaxHops > 3 || group.Robots.Mode != crawlpolicy.RobotsEnforce || group.Robots.OnError != crawlpolicy.RobotsErrorDeny {
			return fmt.Errorf("baseline group %s has unsafe redirect or robots settings", group.ID)
		}
		if !group.Enabled {
			continue
		}
		enabledGroups++
		enabledHostRules += len(group.HostRules)
	}
	if enabledGroups == 0 {
		return fmt.Errorf("baseline policy must contain enabled groups")
	}
	if enabledHostRules != 67 {
		return fmt.Errorf("baseline policy has %d enabled host rules; want 67 reviewed domains", enabledHostRules)
	}
	if policyHash != approvedBaselinePolicySHA256 {
		return fmt.Errorf(
			"baseline policy sha256 is %s; want approved %s",
			policyHash,
			approvedBaselinePolicySHA256,
		)
	}
	return nil
}

func requireSupportedRenderPolicy(policy *renderpolicy.Policy, baseline bool) error {
	if policy == nil {
		return nil
	}
	if baseline && policy.EnabledRuleCount() != 0 {
		return fmt.Errorf("baseline crawl forbids enabled JavaScript render rules")
	}
	for _, rule := range policy.Rules() {
		if !rule.Enabled {
			continue
		}
		switch rule.Mode {
		case renderpolicy.ModeInlineOnly:
			if len(rule.ResourceRules) != 0 || rule.Limits.MaxResourceRequests != 0 ||
				rule.Limits.MaxAggregateResourceBytes != 0 || rule.Limits.MaxResourceBodyBytes != 0 ||
				rule.Limits.MaxRedirectHops != 0 {
				return fmt.Errorf("render policy rule %s has an unsupported inline resource shape", rule.ID)
			}
		case renderpolicy.ModeBrokered:
			if len(rule.ResourceRules) == 0 || rule.Limits.MaxResourceRequests <= 0 ||
				rule.Limits.MaxAggregateResourceBytes <= 0 || rule.Limits.MaxResourceBodyBytes <= 0 ||
				rule.Limits.MaxRedirectHops != 0 {
				return fmt.Errorf("render policy rule %s has an unsupported brokered resource shape", rule.ID)
			}
			for _, resourceRule := range rule.ResourceRules {
				for _, resourceType := range resourceRule.AllowedTypes {
					if resourceType != renderpolicy.ResourceTypeScript && resourceType != renderpolicy.ResourceTypeStylesheet {
						return fmt.Errorf("render policy rule %s authorizes unsupported resource type %s", rule.ID, resourceType)
					}
				}
			}
		default:
			return fmt.Errorf("render policy rule %s uses unsupported mode %s", rule.ID, rule.Mode)
		}
	}
	return nil
}

func requireBaselineExecutionEnvelope(
	validateOnly, once bool,
	maxConcurrency, maxPages int,
	startingURL, userAgent, crawlQueueKey, crawlURLsKey, crawlDepthsKey string,
) error {
	if maxConcurrency > baselineMaxConcurrency || maxPages > baselineMaxPageAttempts {
		return fmt.Errorf(
			"baseline crawl requires --max-concurrency <= %d and --max-pages <= %d",
			baselineMaxConcurrency,
			baselineMaxPageAttempts,
		)
	}
	if !validateOnly && !once {
		return fmt.Errorf("baseline crawl requires --once")
	}
	if !validateOnly && startingURL != "" {
		return fmt.Errorf("baseline crawl forbids STARTING_URL")
	}
	if userAgent != utils.DefaultUserAgent {
		return fmt.Errorf("baseline crawl requires USER_AGENT=%s", utils.DefaultUserAgent)
	}
	if crawlQueueKey != utils.CrawlQueueKeyV1 || crawlURLsKey != utils.CrawlURLsKeyV1 || crawlDepthsKey != utils.CrawlDepthsKeyV1 {
		return fmt.Errorf("baseline crawl requires the V1 queue, URL, and depth keys")
	}
	return nil
}

func splitOptionalList(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			return nil, fmt.Errorf("contains an empty entry")
		}
		result = append(result, trimmed)
	}
	return result, nil
}
