package robotsguard

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawlpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/securefetch"
	"github.com/temoto/robotstxt"
)

const (
	// MaxRobotsBodyBytes is the hard response-body limit for robots.txt.
	MaxRobotsBodyBytes int64 = 512 << 10
	maxCacheEntries          = 32
)

// Manager owns the process-local robots cache. It is safe for concurrent use.
// A Manager snapshots its policy and fetcher references at construction and
// does not hot-reload either one.
type Manager struct {
	policy  *crawlpolicy.Policy
	fetcher *securefetch.Fetcher
	now     func() time.Time

	mu    sync.Mutex
	cache map[cacheKey]*cacheEntry
}

type cacheKey struct {
	origin    string
	userAgent string
}

type cacheEntry struct {
	ready     chan struct{}
	value     cachedPolicy
	expiresAt time.Time
}

type cachedPolicy struct {
	robots    *robotstxt.RobotsData
	allowed   bool
	err       error
	transient bool
}

type robotsRequest struct {
	url       string
	host      string
	target    string
	key       cacheKey
	userAgent string
	robots    crawlpolicy.RobotsPolicy
}

// NewManager constructs a process-local robots manager. Nil dependencies are
// retained so that Allowed can reject the operation with a typed, fail-closed
// error rather than allowing a crawl or panicking.
func NewManager(policy *crawlpolicy.Policy, fetcher *securefetch.Fetcher) *Manager {
	return &Manager{
		policy:  policy,
		fetcher: fetcher,
		now:     time.Now,
		cache:   make(map[cacheKey]*cacheEntry),
	}
}

// New is a concise alias for NewManager.
func New(policy *crawlpolicy.Policy, fetcher *securefetch.Fetcher) *Manager {
	return NewManager(policy, fetcher)
}

// Allowed ensures a fresh robots policy is cached for the page's canonical
// origin and configured user agent, then evaluates the canonical escaped path
// and raw query. Robots disallow decisions return (false, nil). Fetch, status,
// and parse failures return the group's configured fallback together with a
// typed *Error.
func (m *Manager) Allowed(ctx context.Context, pageDecision crawlpolicy.Decision, gate securefetch.RequestGate) (bool, error) {
	if m == nil || m.policy == nil || m.fetcher == nil || m.now == nil || ctx == nil || isNilGate(gate) {
		return false, newError(ReasonInvalidArgument, crawlpolicy.RobotsErrorDeny, 0, nil)
	}
	if err := ctx.Err(); err != nil {
		return false, newError(ReasonWaitCanceled, crawlpolicy.RobotsErrorDeny, 0, err)
	}

	request, err := m.prepareRequest(pageDecision)
	if err != nil {
		return false, err
	}

	policy, err := m.freshPolicy(ctx, request, gate)
	if err != nil {
		return false, err
	}
	return policy.test(request.target, request.userAgent)
}

// ResetCache discards completed and in-progress entries from the manager's
// cache. It is race-safe and intended for explicit test or crawl-batch
// boundaries only; normal batch transitions should leave the cache intact.
// Calls already in progress still complete for their callers, but their result
// is not reinserted after a reset.
func (m *Manager) ResetCache() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.cache = make(map[cacheKey]*cacheEntry)
	m.mu.Unlock()
}

func (m *Manager) prepareRequest(pageDecision crawlpolicy.Decision) (robotsRequest, error) {
	if pageDecision.Identity.CanonicalURL == "" || pageDecision.URL == nil {
		return robotsRequest{}, newError(ReasonInvalidPageDecision, crawlpolicy.RobotsErrorDeny, 0, nil)
	}

	trustedPage, err := m.policy.Match(pageDecision.Identity.CanonicalURL, 0)
	if err != nil {
		return robotsRequest{}, newError(ReasonInvalidPageDecision, crawlpolicy.RobotsErrorDeny, 0, err)
	}
	if pageDecision.Identity != trustedPage.Identity ||
		pageDecision.URL.String() != trustedPage.URL.String() ||
		pageDecision.Scheme != trustedPage.Scheme ||
		pageDecision.Host != trustedPage.Host ||
		pageDecision.Path != trustedPage.Path ||
		pageDecision.Group.ID != trustedPage.Group.ID ||
		pageDecision.Group.UserAgent != trustedPage.Group.UserAgent ||
		pageDecision.Group.Robots != trustedPage.Group.Robots ||
		pageDecision.MatchedHostRule != trustedPage.MatchedHostRule {
		return robotsRequest{}, newError(ReasonInvalidPageDecision, crawlpolicy.RobotsErrorDeny, 0, nil)
	}

	origin := trustedPage.Scheme + "://" + trustedPage.Host
	robotsURL := origin + "/robots.txt"
	robotsDecision, err := m.policy.MatchRobots(robotsURL)
	if err != nil {
		return robotsRequest{}, newError(ReasonRobotsPolicyDenied, crawlpolicy.RobotsErrorDeny, 0, err)
	}
	if robotsDecision.Identity.CanonicalURL != robotsURL ||
		robotsDecision.Scheme != trustedPage.Scheme ||
		robotsDecision.Host != trustedPage.Host ||
		robotsDecision.Path != "/robots.txt" ||
		robotsDecision.Group.ID != trustedPage.Group.ID ||
		robotsDecision.Group.UserAgent != trustedPage.Group.UserAgent {
		return robotsRequest{}, newError(ReasonRobotsPolicyDenied, crawlpolicy.RobotsErrorDeny, 0, nil)
	}

	robotsPolicy := robotsDecision.Group.Robots
	if robotsPolicy.Mode != crawlpolicy.RobotsEnforce || robotsPolicy.CacheTTL <= 0 ||
		(robotsPolicy.OnError != crawlpolicy.RobotsErrorAllow && robotsPolicy.OnError != crawlpolicy.RobotsErrorDeny) {
		return robotsRequest{}, newError(ReasonRobotsPolicyDenied, crawlpolicy.RobotsErrorDeny, 0, nil)
	}

	target := trustedPage.URL.EscapedPath()
	if target == "" {
		target = "/"
	}
	if trustedPage.URL.ForceQuery || trustedPage.URL.RawQuery != "" {
		target += "?" + trustedPage.URL.RawQuery
	}
	target, err = normalizeREPValue(target)
	if err != nil {
		return robotsRequest{}, newError(ReasonInvalidPageDecision, crawlpolicy.RobotsErrorDeny, 0, err)
	}

	return robotsRequest{
		url:       robotsURL,
		host:      trustedPage.Host,
		target:    target,
		key:       cacheKey{origin: origin, userAgent: robotsDecision.Group.UserAgent},
		userAgent: robotsDecision.Group.UserAgent,
		robots:    robotsPolicy,
	}, nil
}

func (m *Manager) freshPolicy(ctx context.Context, request robotsRequest, gate securefetch.RequestGate) (cachedPolicy, error) {
	for {
		m.mu.Lock()
		now := m.now()
		m.pruneExpiredLocked(now)
		entry, exists := m.cache[request.key]
		if exists && entry.ready != nil {
			ready := entry.ready
			m.mu.Unlock()
			select {
			case <-ctx.Done():
				return cachedPolicy{}, newError(ReasonWaitCanceled, crawlpolicy.RobotsErrorDeny, 0, ctx.Err())
			case <-ready:
				continue
			}
		}

		if exists && now.Before(entry.expiresAt) {
			value := entry.value
			m.mu.Unlock()
			return value, nil
		}

		if !exists {
			if len(m.cache) >= maxCacheEntries && !m.evictOneCompletedLocked() {
				m.mu.Unlock()
				value := fallbackPolicy(
					request.robots.OnError,
					newError(ReasonCacheCapacity, request.robots.OnError, 0, nil),
				)
				return value, nil
			}
			entry = &cacheEntry{}
			m.cache[request.key] = entry
		}
		entry.ready = make(chan struct{})
		m.mu.Unlock()

		value := m.fetchPolicy(ctx, request, gate)
		m.completeLoad(request.key, entry, value, request.robots.CacheTTL)
		return value, nil
	}
}

func (m *Manager) completeLoad(key cacheKey, entry *cacheEntry, value cachedPolicy, ttl time.Duration) {
	m.mu.Lock()
	entry.value = value
	if value.transient {
		entry.expiresAt = time.Time{}
	} else {
		entry.expiresAt = m.now().Add(ttl)
	}
	ready := entry.ready
	entry.ready = nil
	if current, exists := m.cache[key]; !exists || current != entry {
		// ResetCache replaced the map while this load was in progress.
		entry.expiresAt = time.Time{}
	} else if value.transient {
		delete(m.cache, key)
	}
	close(ready)
	m.mu.Unlock()
}

func (m *Manager) fetchPolicy(ctx context.Context, request robotsRequest, gate securefetch.RequestGate) cachedPolicy {
	result, err := m.fetcher.Fetch(ctx, request.url, m.robotsFetchMatcher(request), gate, MaxRobotsBodyBytes)
	if err != nil {
		if ctx.Err() != nil {
			return cachedPolicy{
				allowed:   false,
				err:       newError(ReasonWaitCanceled, crawlpolicy.RobotsErrorDeny, 0, ctx.Err()),
				transient: true,
			}
		}
		fallback := fallbackPolicy(request.robots.OnError, newError(ReasonFetchFailed, request.robots.OnError, 0, err))
		if securefetch.ReasonOf(err) == securefetch.ReasonGateDenied {
			// Capacity denial means no robots network request was attempted.
			// Do not retain the fallback for the full robots TTL: requeued work
			// must be able to retry after the next batch resets its capacity.
			fallback.transient = true
		}
		return fallback
	}

	switch {
	case result.StatusCode >= http.StatusOK && result.StatusCode < http.StatusMultipleChoices:
		parsed, parseErr := parseRobotsResponse(result.Body, result.ContentType, robotstxt.FromBytes)
		if parseErr != nil || parsed == nil {
			return fallbackPolicy(request.robots.OnError, newError(ReasonParseFailed, request.robots.OnError, 0, parseErr))
		}
		return cachedPolicy{robots: parsed}
	case result.StatusCode == http.StatusNotFound || result.StatusCode == http.StatusGone:
		return cachedPolicy{allowed: true}
	case result.StatusCode == http.StatusUnauthorized || result.StatusCode == http.StatusForbidden:
		return cachedPolicy{allowed: false}
	default:
		robotsErr := newError(ReasonUnexpectedStatus, request.robots.OnError, result.StatusCode, nil)
		return fallbackPolicy(request.robots.OnError, robotsErr)
	}
}

func (m *Manager) robotsFetchMatcher(request robotsRequest) securefetch.Matcher {
	initial := true
	return func(rawURL string) (crawlpolicy.Decision, error) {
		if initial {
			initial = false
			if rawURL != request.url {
				return crawlpolicy.Decision{}, errors.New("robots initial target mismatch")
			}
			decision, err := m.policy.MatchRobots(rawURL)
			if err != nil {
				return decision, err
			}
			if decision.Identity.CanonicalURL != request.url || decision.Host != request.host || decision.Path != "/robots.txt" {
				return crawlpolicy.Decision{}, errors.New("robots initial policy mismatch")
			}
			return decision, nil
		}

		decision, err := m.policy.Match(rawURL, 0)
		if err != nil {
			return decision, err
		}
		if decision.Host != request.host {
			return crawlpolicy.Decision{}, errors.New("robots redirect changed host")
		}
		return decision, nil
	}
}

func (m *Manager) pruneExpiredLocked(now time.Time) {
	for key, entry := range m.cache {
		if entry.ready == nil && !entry.expiresAt.IsZero() && !now.Before(entry.expiresAt) {
			delete(m.cache, key)
		}
	}
}

func (m *Manager) evictOneCompletedLocked() bool {
	var selected cacheKey
	var selectedEntry *cacheEntry
	for key, entry := range m.cache {
		if entry.ready != nil {
			continue
		}
		if selectedEntry == nil || entry.expiresAt.Before(selectedEntry.expiresAt) {
			selected = key
			selectedEntry = entry
		}
	}
	if selectedEntry == nil {
		return false
	}
	delete(m.cache, selected)
	return true
}

func fallbackPolicy(action crawlpolicy.RobotsErrorAction, err error) cachedPolicy {
	return cachedPolicy{
		allowed: action == crawlpolicy.RobotsErrorAllow,
		err:     err,
	}
}

func (policy cachedPolicy) test(target, userAgent string) (bool, error) {
	if policy.robots != nil {
		return policy.robots.TestAgent(target, userAgent), nil
	}
	if robotsErr, ok := policy.err.(*Error); ok {
		// Do not expose the mutable error object retained by the shared cache.
		return policy.allowed, newError(robotsErr.Reason, robotsErr.Fallback, robotsErr.StatusCode, robotsErr.Cause)
	}
	return policy.allowed, policy.err
}

func isNilGate(gate securefetch.RequestGate) bool {
	if gate == nil {
		return true
	}
	value := reflect.ValueOf(gate)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
