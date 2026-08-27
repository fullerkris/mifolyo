package crawler

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawlpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/renderclient"
	"github.com/IonelPopJara/search-engine/services/spider/internal/renderpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/securefetch"
	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

const maxBrokerContentTypeBytes = 1024

var ErrResourceDenied = errors.New("brokered resource denied")

// pageResourceBroker is an immutable authorization snapshot for one fetched
// page. Only its request and approved-byte counters change after construction.
type pageResourceBroker struct {
	effectiveURL string
	rule         renderpolicy.Rule
	depth        int
	pageDecision crawlpolicy.Decision
	policy       *crawlpolicy.Policy
	gate         *crawlRequestGate
	fetcher      *securefetch.Fetcher
	robots       RobotsAuthorizer

	mu                 sync.Mutex
	requestCount       int
	successfulRequests int
	approvedBytes      int64
}

func (crawcfg *CrawlerConfig) newPageResourceBroker(
	effectiveURL string,
	rule renderpolicy.Rule,
	depth int,
	pageDecision crawlpolicy.Decision,
	gate *crawlRequestGate,
) (*pageResourceBroker, error) {
	if crawcfg == nil || crawcfg.RenderPolicy == nil || crawcfg.Policy == nil || crawcfg.Fetcher == nil || isNilBrokerDependency(crawcfg.Robots) || gate == nil {
		return nil, fmt.Errorf("%w: broker dependencies are not configured", ErrResourceDenied)
	}
	trustedRule, err := crawcfg.RenderPolicy.Match(effectiveURL)
	if err != nil || !reflect.DeepEqual(rule, trustedRule) {
		return nil, fmt.Errorf("%w: matched render rule is invalid", ErrResourceDenied)
	}
	trustedPageDecision, err := crawcfg.Policy.Match(effectiveURL, depth)
	if err != nil || !reflect.DeepEqual(pageDecision, trustedPageDecision) {
		return nil, fmt.Errorf("%w: originating page decision is invalid", ErrResourceDenied)
	}
	trustedGroup, groupExists := crawcfg.Policy.Group(trustedPageDecision.Group.ID)
	if !validBrokerPageDecision(effectiveURL, trustedPageDecision) || !groupExists ||
		!sameBrokerGroup(trustedPageDecision.Group, trustedGroup) || !gate.validBrokerBinding(crawcfg, trustedPageDecision.Group.ID) {
		return nil, fmt.Errorf("%w: originating page binding is invalid", ErrResourceDenied)
	}
	if trustedRule.Mode != renderpolicy.ModeBrokered || !trustedRule.Enabled || len(trustedRule.ResourceRules) == 0 ||
		trustedRule.Limits.MaxResourceRequests <= 0 || trustedRule.Limits.MaxAggregateResourceBytes <= 0 ||
		trustedRule.Limits.MaxResourceBodyBytes <= 0 || trustedRule.Limits.MaxRedirectHops != 0 {
		return nil, fmt.Errorf("%w: matched render rule cannot broker resources", ErrResourceDenied)
	}

	return &pageResourceBroker{
		effectiveURL: effectiveURL,
		rule:         cloneBrokerRule(trustedRule),
		depth:        depth,
		pageDecision: cloneBrokerDecision(trustedPageDecision),
		policy:       crawcfg.Policy,
		gate:         gate,
		fetcher:      crawcfg.Fetcher,
		robots:       crawcfg.Robots,
	}, nil
}

func (broker *pageResourceBroker) Fetch(ctx context.Context, intent renderclient.ResourceIntent) (renderclient.Resource, error) {
	if broker == nil || ctx == nil {
		return renderclient.Resource{}, fmt.Errorf("%w: broker context is invalid", ErrResourceDenied)
	}
	if err := ctx.Err(); err != nil {
		return renderclient.Resource{}, fmt.Errorf("%w: resource context: %w", ErrResourceDenied, err)
	}
	if intent.Method != http.MethodGet {
		return renderclient.Resource{}, fmt.Errorf("%w: resource method must be GET", ErrResourceDenied)
	}
	if intent.Type != renderpolicy.ResourceTypeScript && intent.Type != renderpolicy.ResourceTypeStylesheet {
		return renderclient.Resource{}, fmt.Errorf("%w: resource type is unsupported", ErrResourceDenied)
	}

	canonicalURL, err := requireCanonicalHTTPSResourceURL(intent.URL)
	if err != nil {
		return renderclient.Resource{}, fmt.Errorf("%w: resource URL: %w", ErrResourceDenied, err)
	}
	matchedURL, err := broker.rule.MatchResource(canonicalURL, intent.Type)
	if err != nil || matchedURL != canonicalURL {
		if err == nil {
			err = errors.New("render policy changed the resource URL")
		}
		return renderclient.Resource{}, fmt.Errorf("%w: render policy: %w", ErrResourceDenied, err)
	}

	resourceDecision, err := broker.policy.Match(canonicalURL, broker.depth)
	if err != nil {
		return renderclient.Resource{}, fmt.Errorf("%w: crawl policy: %w", ErrResourceDenied, err)
	}
	if !sameBrokerGroup(resourceDecision.Group, broker.pageDecision.Group) {
		return renderclient.Resource{}, fmt.Errorf("%w: resource changed crawl-policy group", ErrResourceDenied)
	}
	if err := broker.reserveRequest(); err != nil {
		return renderclient.Resource{}, err
	}

	maxBodyBytes, err := broker.fetchBodyLimit()
	if err != nil {
		return renderclient.Resource{}, err
	}
	matcher := func(rawURL string) (crawlpolicy.Decision, error) {
		if rawURL != canonicalURL {
			return crawlpolicy.Decision{}, errors.New("direct resource target changed")
		}
		return cloneBrokerDecision(resourceDecision), nil
	}
	authorizer := func(authContext context.Context, decision crawlpolicy.Decision, _ securefetch.RequestGate) error {
		if decision.Identity != resourceDecision.Identity || !sameBrokerGroup(decision.Group, broker.pageDecision.Group) {
			return fmt.Errorf("%w: secure fetch decision changed", ErrResourceDenied)
		}
		allowed, robotsErr := broker.robots.Allowed(authContext, decision, broker.gate)
		if robotsErr != nil {
			return fmt.Errorf("%w: robots authorization: %w", ErrResourceDenied, robotsErr)
		}
		if !allowed {
			return ErrRobotsDenied
		}
		return nil
	}

	result, err := broker.fetcher.FetchAuthorizedDirect(
		ctx,
		canonicalURL,
		matcher,
		broker.gate,
		maxBodyBytes,
		authorizer,
	)
	if err != nil {
		return renderclient.Resource{}, fmt.Errorf("%w: direct fetch: %w", ErrResourceDenied, err)
	}
	if result.StatusCode != http.StatusOK || result.EffectiveURL != canonicalURL || len(result.RedirectChain) != 0 ||
		result.Decision.Identity != resourceDecision.Identity || !sameBrokerGroup(result.Decision.Group, broker.pageDecision.Group) {
		return renderclient.Resource{}, fmt.Errorf("%w: direct response identity or status is invalid", ErrResourceDenied)
	}

	contentType, err := normalizeBrokerContentType(result.ContentType, result.ContentTypeValues, intent.Type)
	if err != nil {
		return renderclient.Resource{}, fmt.Errorf("%w: %w", ErrResourceDenied, err)
	}
	if len(result.Body) == 0 || !utf8.Valid(result.Body) {
		return renderclient.Resource{}, fmt.Errorf("%w: resource body must be non-empty valid UTF-8", ErrResourceDenied)
	}
	if err := broker.commitResource(int64(len(result.Body))); err != nil {
		return renderclient.Resource{}, err
	}

	return renderclient.Resource{
		Body:        append([]byte(nil), result.Body...),
		ContentType: contentType,
	}, nil
}

func (broker *pageResourceBroker) reserveRequest() error {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	if broker.requestCount >= broker.rule.Limits.MaxResourceRequests {
		return fmt.Errorf("%w: resource request-count limit exceeded", ErrResourceDenied)
	}
	broker.requestCount++
	return nil
}

func (broker *pageResourceBroker) fetchBodyLimit() (int64, error) {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	remaining := broker.rule.Limits.MaxAggregateResourceBytes - broker.approvedBytes
	if remaining <= 0 {
		return 0, fmt.Errorf("%w: aggregate resource-byte limit exhausted", ErrResourceDenied)
	}
	if remaining < broker.rule.Limits.MaxResourceBodyBytes {
		return remaining, nil
	}
	return broker.rule.Limits.MaxResourceBodyBytes, nil
}

func (broker *pageResourceBroker) commitResource(bodyBytes int64) error {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	if bodyBytes <= 0 || bodyBytes > broker.rule.Limits.MaxResourceBodyBytes {
		return fmt.Errorf("%w: per-resource byte limit exceeded", ErrResourceDenied)
	}
	if broker.approvedBytes > broker.rule.Limits.MaxAggregateResourceBytes ||
		bodyBytes > broker.rule.Limits.MaxAggregateResourceBytes-broker.approvedBytes {
		return fmt.Errorf("%w: aggregate resource-byte limit exceeded", ErrResourceDenied)
	}
	broker.successfulRequests++
	broker.approvedBytes += bodyBytes
	return nil
}

func normalizeBrokerContentType(contentType string, values []string, resourceType renderpolicy.ResourceType) (string, error) {
	if len(values) != 1 || values[0] == "" || values[0] != contentType || len(values[0]) > maxBrokerContentTypeBytes ||
		!utf8.ValidString(values[0]) || strings.TrimSpace(values[0]) != values[0] {
		return "", errors.New("resource Content-Type must contain exactly one unambiguous value")
	}
	mediaType, parameters, err := mime.ParseMediaType(values[0])
	if err != nil || mediaType == "" {
		return "", errors.New("resource Content-Type is malformed")
	}

	allowed := false
	switch resourceType {
	case renderpolicy.ResourceTypeScript:
		allowed = mediaType == "application/javascript" || mediaType == "text/javascript"
	case renderpolicy.ResourceTypeStylesheet:
		allowed = mediaType == "text/css"
	}
	if !allowed {
		return "", errors.New("resource Content-Type is not allowed for its type")
	}
	if charset, present := parameters["charset"]; present && !strings.EqualFold(charset, "utf-8") {
		return "", errors.New("resource Content-Type charset must be UTF-8")
	}
	return mediaType, nil
}

func requireCanonicalHTTPSResourceURL(rawURL string) (string, error) {
	identity, err := utils.CanonicalizeURLV1(rawURL)
	if err != nil {
		return "", err
	}
	if rawURL != identity.CanonicalURL {
		return "", errors.New("URL must already be canonical")
	}
	if err := utils.RequireStaticCrawlEligibility(identity); err != nil {
		return "", err
	}
	parsed, err := url.Parse(identity.CanonicalURL)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" || parsed.Port() != "" ||
		parsed.Fragment != "" || parsed.RawFragment != "" || !utils.IsUnambiguousFetchPath(parsed) {
		return "", errors.New("URL is outside the canonical HTTPS resource envelope")
	}
	return identity.CanonicalURL, nil
}

func validBrokerPageDecision(effectiveURL string, decision crawlpolicy.Decision) bool {
	identity, err := utils.CanonicalizeURLV1(effectiveURL)
	if err != nil || identity.CanonicalURL != effectiveURL || decision.Identity != identity || decision.URL == nil ||
		decision.URL.String() != effectiveURL || decision.Group.ID == "" {
		return false
	}
	parsed, err := url.Parse(identity.CanonicalURL)
	return err == nil && parsed.Scheme == "https" && decision.Scheme == parsed.Scheme &&
		decision.Host == parsed.Hostname() && decision.Path == parsed.EscapedPath()
}

func sameBrokerGroup(left, right crawlpolicy.Group) bool {
	return left.ID != "" && reflect.DeepEqual(left, right)
}

func cloneBrokerDecision(decision crawlpolicy.Decision) crawlpolicy.Decision {
	if decision.URL != nil {
		clonedURL := *decision.URL
		decision.URL = &clonedURL
	}
	decision.Group.HostRules = append([]crawlpolicy.HostRule(nil), decision.Group.HostRules...)
	decision.Group.AllowedSchemes = append([]string(nil), decision.Group.AllowedSchemes...)
	decision.Group.AllowPathPrefixes = append([]string(nil), decision.Group.AllowPathPrefixes...)
	decision.Group.DenyPathPrefixes = append([]string(nil), decision.Group.DenyPathPrefixes...)
	return decision
}

func cloneBrokerRule(rule renderpolicy.Rule) renderpolicy.Rule {
	rule.AllowPaths = append([]string(nil), rule.AllowPaths...)
	rule.AllowPathPrefixes = append([]string(nil), rule.AllowPathPrefixes...)
	rule.DenyPathPrefixes = append([]string(nil), rule.DenyPathPrefixes...)
	rule.ResourceRules = append([]renderpolicy.ResourceRule(nil), rule.ResourceRules...)
	for index := range rule.ResourceRules {
		rule.ResourceRules[index].AllowPaths = append([]string(nil), rule.ResourceRules[index].AllowPaths...)
		rule.ResourceRules[index].AllowPathPrefixes = append([]string(nil), rule.ResourceRules[index].AllowPathPrefixes...)
		rule.ResourceRules[index].DenyPathPrefixes = append([]string(nil), rule.ResourceRules[index].DenyPathPrefixes...)
		rule.ResourceRules[index].AllowedTypes = append([]renderpolicy.ResourceType(nil), rule.ResourceRules[index].AllowedTypes...)
	}
	return rule
}

func isNilBrokerDependency(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

var _ renderclient.ResourceBroker = (*pageResourceBroker)(nil)
