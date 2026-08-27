package crawler

import (
	"context"
	"errors"
	"sync"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawlpolicy"
)

var ErrGlobalRequestLimit = errors.New("global crawl request limit reached")

var ErrRequestGateClosed = errors.New("crawl request gate is closed")

type crawlRequestGate struct {
	crawler             *CrawlerConfig
	initialGroup        string
	firstGroupCapacity  *crawlpolicy.RequestReservation
	firstGlobalCapacity *requestBudgetReservation
	mu                  sync.Mutex
	closed              bool
}

func newCrawlRequestGate(crawler *CrawlerConfig, groupID string, groupCapacity *crawlpolicy.RequestReservation, globalCapacity *requestBudgetReservation) *crawlRequestGate {
	return &crawlRequestGate{
		crawler:             crawler,
		initialGroup:        groupID,
		firstGroupCapacity:  groupCapacity,
		firstGlobalCapacity: globalCapacity,
	}
}

func (gate *crawlRequestGate) Acquire(ctx context.Context, decision crawlpolicy.Decision) (func(), error) {
	gate.mu.Lock()
	if gate.closed {
		gate.mu.Unlock()
		return nil, ErrRequestGateClosed
	}
	if decision.Group.ID != gate.initialGroup {
		gate.mu.Unlock()
		return nil, crawlpolicy.ErrUnknownGroup
	}
	if gate.firstGroupCapacity != nil && gate.firstGlobalCapacity != nil {
		if err := ctx.Err(); err != nil {
			groupCapacity := gate.firstGroupCapacity
			globalCapacity := gate.firstGlobalCapacity
			gate.firstGroupCapacity = nil
			gate.firstGlobalCapacity = nil
			gate.mu.Unlock()
			groupCapacity.Refund()
			globalCapacity.refund()
			return nil, err
		}
		groupCapacity := gate.firstGroupCapacity
		globalCapacity := gate.firstGlobalCapacity
		gate.firstGroupCapacity = nil
		gate.firstGlobalCapacity = nil
		gate.mu.Unlock()
		groupRelease, err := groupCapacity.Commit()
		if err != nil {
			groupCapacity.Refund()
			globalCapacity.refund()
			return nil, err
		}
		globalCapacity.commit()
		return groupRelease, nil
	}
	globalCapacity, reserved := gate.crawler.reservePageAttempt()
	if !reserved {
		gate.mu.Unlock()
		return nil, ErrGlobalRequestLimit
	}
	groupRelease, err := gate.crawler.PolicyRuntime.Acquire(ctx, decision.Group.ID)
	if err != nil {
		globalCapacity.refund()
		gate.mu.Unlock()
		return nil, err
	}
	globalCapacity.commit()
	gate.mu.Unlock()
	return groupRelease, nil
}

func (gate *crawlRequestGate) Close() {
	gate.mu.Lock()
	if gate.closed {
		gate.mu.Unlock()
		return
	}
	gate.closed = true
	groupCapacity := gate.firstGroupCapacity
	globalCapacity := gate.firstGlobalCapacity
	gate.firstGroupCapacity = nil
	gate.firstGlobalCapacity = nil
	gate.mu.Unlock()
	if groupCapacity != nil {
		groupCapacity.Refund()
	}
	if globalCapacity != nil {
		globalCapacity.refund()
	}
}

func (gate *crawlRequestGate) validBrokerBinding(crawler *CrawlerConfig, groupID string) bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return !gate.closed && gate.crawler == crawler && groupID != "" && gate.initialGroup == groupID
}

func (gate *crawlRequestGate) closeUnused() {
	gate.Close()
}
