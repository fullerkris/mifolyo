package crawler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawlpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/database"
)

var (
	ErrNoPendingURLs     = errors.New("no pending crawl URLs")
	ErrNoPolicyCapacity  = errors.New("no domain group has request capacity in this batch")
	ErrPendingURLInvalid = errors.New("pending crawl URL is invalid")
)

type crawlJob struct {
	candidate           database.CrawlCandidate
	decision            crawlpolicy.Decision
	firstGroupCapacity  *crawlpolicy.RequestReservation
	firstGlobalCapacity *requestBudgetReservation
}

func (job *crawlJob) releaseUnusedFirstHop() {
	if job == nil {
		return
	}
	if job.firstGroupCapacity != nil {
		job.firstGroupCapacity.Refund()
		job.firstGroupCapacity = nil
	}
	if job.firstGlobalCapacity != nil {
		job.firstGlobalCapacity.refund()
		job.firstGlobalCapacity = nil
	}
}

type scheduledCandidate struct {
	candidate database.CrawlCandidate
	decision  crawlpolicy.Decision
}

func (crawcfg *CrawlerConfig) claimNextURL(ctx context.Context, db *database.Database) (crawlJob, error) {
	if crawcfg.Policy == nil || crawcfg.PolicyRuntime == nil {
		return crawlJob{}, fmt.Errorf("crawl policy is not configured")
	}

	for {
		if err := ctx.Err(); err != nil {
			return crawlJob{}, err
		}
		runtimeChanged := crawcfg.PolicyRuntime.Changes()
		crawcfg.scheduleMu.Lock()
		candidates, err := db.ListPendingURLs()
		if err != nil {
			crawcfg.scheduleMu.Unlock()
			return crawlJob{}, err
		}
		if len(candidates) == 0 {
			crawcfg.scheduleMu.Unlock()
			return crawlJob{}, ErrNoPendingURLs
		}

		var best *scheduledCandidate
		var nextStart time.Time
		temporarilyBlocked := false
		for _, candidate := range candidates {
			if candidate.ValidationError != nil {
				crawcfg.scheduleMu.Unlock()
				return crawlJob{}, fmt.Errorf("%w: %v", ErrPendingURLInvalid, candidate.ValidationError)
			}

			decision, matchErr := crawcfg.Policy.Match(candidate.CanonicalURL, candidate.Depth)
			if matchErr != nil {
				continue
			}

			availability, readyErr := crawcfg.PolicyRuntime.Ready(decision.Group.ID, time.Now())
			if readyErr != nil {
				crawcfg.scheduleMu.Unlock()
				return crawlJob{}, readyErr
			}
			if !availability.Ready {
				if temporaryAvailability(availability.Reason) {
					temporarilyBlocked = true
					if !availability.NextStart.IsZero() && (nextStart.IsZero() || availability.NextStart.Before(nextStart)) {
						nextStart = availability.NextStart
					}
				}
				continue
			}
			scheduled := scheduledCandidate{candidate: candidate, decision: decision}
			if best == nil || scheduledBefore(scheduled, *best) {
				best = &scheduled
			}
		}

		if best != nil {
			if err := ctx.Err(); err != nil {
				crawcfg.scheduleMu.Unlock()
				return crawlJob{}, err
			}
			globalCapacity, reserved := crawcfg.reservePageAttempt()
			if !reserved {
				crawcfg.scheduleMu.Unlock()
				return crawlJob{}, ErrGlobalRequestLimit
			}

			groupCapacity, availability, reserveErr := crawcfg.PolicyRuntime.TryReserve(best.decision.Group.ID, time.Now())
			if reserveErr != nil {
				globalCapacity.refund()
				crawcfg.scheduleMu.Unlock()
				return crawlJob{}, reserveErr
			}
			if groupCapacity == nil {
				globalCapacity.refund()
				crawcfg.scheduleMu.Unlock()
				if temporaryAvailability(availability.Reason) {
					if err := waitForSchedulerCapacity(ctx, runtimeChanged, availability.NextStart); err != nil {
						return crawlJob{}, err
					}
					continue
				}
				continue
			}

			claimed, claimErr := claimURLWithReservedCapacity(db, best.candidate, groupCapacity, globalCapacity)
			if claimErr != nil {
				crawcfg.scheduleMu.Unlock()
				return crawlJob{}, claimErr
			}
			if !claimed {
				crawcfg.scheduleMu.Unlock()
				continue
			}
			crawcfg.scheduleMu.Unlock()
			return crawlJob{
				candidate:           best.candidate,
				decision:            best.decision,
				firstGroupCapacity:  groupCapacity,
				firstGlobalCapacity: globalCapacity,
			}, nil
		}

		crawcfg.scheduleMu.Unlock()
		if !temporarilyBlocked {
			return crawlJob{}, ErrNoPolicyCapacity
		}
		if err := waitForSchedulerCapacity(ctx, runtimeChanged, nextStart); err != nil {
			return crawlJob{}, err
		}
	}
}

func temporaryAvailability(reason crawlpolicy.AvailabilityReason) bool {
	return reason == crawlpolicy.AvailabilityConcurrency ||
		reason == crawlpolicy.AvailabilityRate ||
		reason == crawlpolicy.AvailabilityPending
}

func waitForSchedulerCapacity(ctx context.Context, changed <-chan struct{}, nextStart time.Time) error {
	if nextStart.IsZero() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
			return nil
		}
	}

	delay := time.Until(nextStart)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-changed:
		return nil
	case <-timer.C:
		return nil
	}
}

func claimURLWithReservedCapacity(
	db *database.Database,
	candidate database.CrawlCandidate,
	groupCapacity *crawlpolicy.RequestReservation,
	globalCapacity *requestBudgetReservation,
) (bool, error) {
	claimed, err := db.ClaimURL(candidate)
	if err != nil || !claimed {
		groupCapacity.Refund()
		globalCapacity.refund()
	}
	return claimed, err
}

func scheduledBefore(left, right scheduledCandidate) bool {
	if left.decision.Group.Priority != right.decision.Group.Priority {
		return left.decision.Group.Priority < right.decision.Group.Priority
	}
	if left.decision.Group.ID != right.decision.Group.ID {
		return left.decision.Group.ID < right.decision.Group.ID
	}
	if left.candidate.Score != right.candidate.Score {
		return left.candidate.Score < right.candidate.Score
	}
	return left.candidate.URLID < right.candidate.URLID
}
