package crawler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawlpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/database"
	"github.com/IonelPopJara/search-engine/services/spider/internal/pages"
	"github.com/IonelPopJara/search-engine/services/spider/internal/renderclient"
	"github.com/IonelPopJara/search-engine/services/spider/internal/securefetch"
	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

var ErrRobotsDenied = errors.New("robots policy denied page request")

// BFS crawling
func (crawcfg *CrawlerConfig) Crawl(db *database.Database) {
	// Starting a new webcrawler instance
	defer crawcfg.Wg.Done()

	ctx := context.Background()

	for {
		job, err := crawcfg.claimNextURL(ctx, db)
		if err != nil {
			if errors.Is(err, ErrNoPendingURLs) || errors.Is(err, ErrNoPolicyCapacity) || errors.Is(err, ErrGlobalRequestLimit) {
				log.Printf("Crawl scheduler stopped this batch: %v\n", err)
				return
			}
			log.Printf("Crawl scheduler failed: %v\n", err)
			crawcfg.recordBatchError(err)
			return
		}
		gate := newCrawlRequestGate(
			crawcfg,
			job.decision.Group.ID,
			job.firstGroupCapacity,
			job.firstGlobalCapacity,
		)
		job.firstGroupCapacity = nil
		job.firstGlobalCapacity = nil

		log.Printf(
			"Claimed URL ID: %s | Depth: %d | Group: %s | Host: %s\n",
			job.candidate.URLID,
			job.candidate.Depth,
			job.decision.Group.ID,
			job.decision.Host,
		)

		// Check if the URL has been visited
		visited, err := db.HasURLBeenVisited(job.candidate.CanonicalURL)
		if err != nil {
			gate.Close()
			log.Printf("Error: [%v] - skipping...\n", err)
			crawcfg.recordBatchError(err)
			continue
		}

		if visited {
			gate.Close()
			log.Printf("Skipping URL ID %s - already visited\n", job.candidate.URLID)
			continue
		}
		if crawcfg.processedAtSameOrShallowerDepth(job.candidate.CanonicalURL, job.candidate.Depth) {
			gate.Close()
			log.Printf("Skipping URL ID %s - already processed at the same or a shallower depth\n", job.candidate.URLID)
			continue
		}

		if crawcfg.Robots == nil {
			gate.Close()
			log.Printf("Robots enforcement is not configured; refusing URL ID %s\n", job.candidate.URLID)
			continue
		}
		depth := job.candidate.Depth
		data, err := getPageData(
			ctx,
			crawcfg.Fetcher,
			job.candidate.CanonicalURL,
			func(rawURL string) (crawlpolicy.Decision, error) {
				return crawcfg.Policy.Match(rawURL, depth)
			},
			gate,
			func(authContext context.Context, decision crawlpolicy.Decision, requestGate securefetch.RequestGate) error {
				allowed, robotsErr := crawcfg.Robots.Allowed(authContext, decision, requestGate)
				if robotsErr != nil {
					log.Printf("Robots policy fallback for URL ID %s: %v\n", decision.Identity.URLID, robotsErr)
				}
				if allowed {
					return nil
				}
				if robotsErr != nil {
					return robotsErr
				}
				return ErrRobotsDenied
			},
		)
		if err != nil {
			gate.Close()
			log.Printf("Error fetching URL ID %s: %v\n", job.candidate.URLID, err)
			if shouldRequeueAfterCapacityFailure(err) {
				if requeueErr := db.PushURLWithDepth(
					job.candidate.CanonicalURL,
					job.candidate.Score,
					job.candidate.Depth,
				); requeueErr != nil {
					crawcfg.recordBatchError(fmt.Errorf("requeue URL ID %s after request-capacity denial: %w", job.candidate.URLID, requeueErr))
				} else {
					log.Printf("Requeued URL ID %s after request-capacity denial\n", job.candidate.URLID)
				}
				return
			}
			continue
		}
		originalHTML := data.HTML
		rendered, err := func() (pageRenderResult, error) {
			defer gate.Close()
			return crawcfg.renderIfRequired(ctx, data.EffectiveURL, originalHTML, &pageRenderBinding{
				Depth:    depth,
				Decision: data.Decision,
				Gate:     gate,
			})
		}()
		if err != nil {
			log.Printf("Error rendering URL ID %s: %v\n", job.candidate.URLID, err)
			if renderclient.IsTemporary(err) || shouldRequeueAfterCapacityFailure(err) {
				if requeueErr := db.PushURLWithDepth(
					job.candidate.CanonicalURL,
					job.candidate.Score,
					job.candidate.Depth,
				); requeueErr != nil {
					crawcfg.recordBatchError(fmt.Errorf("requeue URL ID %s after transient render failure: %w", job.candidate.URLID, requeueErr))
				} else {
					log.Printf("Requeued URL ID %s after transient render failure\n", job.candidate.URLID)
				}
				return
			}
			continue
		}
		data.HTML = rendered.HTML
		aliases := append([]string{job.candidate.CanonicalURL}, data.RedirectChain...)
		aliases = append(aliases, data.EffectiveURL)
		currentAliases := make(map[string]struct{}, len(aliases))
		for _, alias := range aliases {
			currentAliases[alias] = struct{}{}
		}

		outgoingLinks, imagesMap, err := getURLsFromHTML(data.HTML, data.EffectiveURL)
		if err != nil {
			log.Printf("Error getting links from HTML: %v\n", err)
			continue
		}
		if err := validateOutgoingLinkCount(outgoingLinks); err != nil {
			log.Printf("Defensive outgoing-link limit check failed: %v\n", err)
			crawcfg.recordBatchError(err)
			continue
		}

		crawcfg.AddImages(data.EffectiveURL, imagesMap, depth)
		crawcfg.UpdateLinks(data.EffectiveURL, outgoingLinks)

		pg := pages.CreatePage(data.EffectiveURL, data.HTML, data.ContentType, data.StatusCode)
		if rendered.RuleID != "" {
			pg = pages.CreateRenderedPage(
				data.EffectiveURL,
				originalHTML,
				data.HTML,
				data.ContentType,
				data.StatusCode,
				rendered.RuleID,
				rendered.PolicySHA256,
			)
		}

		err = crawcfg.addPage(pg)
		if err != nil {
			log.Printf("\tError adding page visit: %v\n", err)
			continue
		}

		err = db.VisitPage(data.EffectiveURL)
		if err != nil {
			log.Printf("\tError adding page visit: %v\n", err)
			crawcfg.recordBatchError(err)
			continue
		}

		nextDepth := depth + 1
		for _, outgoingLink := range outgoingLinks {
			decision, matchErr := crawcfg.Policy.Match(outgoingLink, nextDepth)
			if matchErr != nil {
				continue
			}
			if _, currentAlias := currentAliases[decision.Identity.CanonicalURL]; currentAlias {
				continue
			}
			if crawcfg.processedAtSameOrShallowerDepth(decision.Identity.CanonicalURL, nextDepth) {
				continue
			}

			score, exists, err := db.ExistsInQueue(decision.Identity.URLID)
			if err != nil {
				log.Printf("Error checking queue for URL ID %s: %v\n", decision.Identity.URLID, err)
				crawcfg.recordBatchError(fmt.Errorf("check discovered URL ID %s in crawl queue: %w", decision.Identity.URLID, err))
				continue
			}
			if !exists {
				score = job.candidate.Score + 1
			}

			score = math.Max(utils.MinScore, math.Min(score, utils.MaxScore))
			if err := db.PushURLWithDepth(decision.Identity.CanonicalURL, score, nextDepth); err != nil {
				log.Printf("Error queueing URL ID %s: %v\n", decision.Identity.URLID, err)
				crawcfg.recordBatchError(fmt.Errorf("queue discovered URL ID %s: %w", decision.Identity.URLID, err))
			}
		}
		crawcfg.recordAliasesAtDepth(depth, aliases...)
	}
}

func shouldRequeueAfterCapacityFailure(err error) bool {
	if errors.Is(err, ErrGlobalRequestLimit) || errors.Is(err, crawlpolicy.ErrBatchLimitReached) {
		return true
	}
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	reason := securefetch.ReasonOf(err)
	return reason == securefetch.ReasonGateDenied || reason == securefetch.ReasonHopDenied
}
