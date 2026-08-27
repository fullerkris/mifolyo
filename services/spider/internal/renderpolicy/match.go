package renderpolicy

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

func (p *Policy) Match(rawURL string) (Rule, error) {
	identity, err := utils.CanonicalizeURLV1(rawURL)
	if err != nil {
		return Rule{}, fmt.Errorf("render URL is invalid: %w", err)
	}
	if err := utils.RequireStaticCrawlEligibility(identity); err != nil {
		return Rule{}, fmt.Errorf("render URL is not statically eligible: %w", err)
	}
	parsed, err := url.Parse(identity.CanonicalURL)
	if err != nil {
		return Rule{}, fmt.Errorf("render URL has an ambiguous path")
	}
	if parsed.Scheme != "https" {
		return Rule{}, ErrNoMatchingRule
	}
	if !utils.IsUnambiguousFetchPath(parsed) {
		return Rule{}, fmt.Errorf("render URL has an ambiguous path")
	}
	if p == nil {
		return Rule{}, ErrNoMatchingRule
	}
	path := parsed.EscapedPath()
	for _, rule := range p.rules {
		if !rule.Enabled || parsed.Hostname() != rule.Host {
			continue
		}
		if pathHasPrefix(path, rule.DenyPathPrefixes) {
			return Rule{}, ErrNoMatchingRule
		}
		if contains(rule.AllowPaths, path) || pathHasPrefix(path, rule.AllowPathPrefixes) {
			return cloneRule(rule), nil
		}
	}
	return Rule{}, ErrNoMatchingRule
}

// MatchResource authorizes a canonical HTTPS script or stylesheet against the
// resource allowlist retained on a matched brokered rule. It never normalizes a
// request on the caller's behalf: rawURL must already be the exact V1 canonical
// URL that is returned on success.
func (rule Rule) MatchResource(rawURL string, resourceType ResourceType) (string, error) {
	if !rule.Enabled || rule.Mode != ModeBrokered {
		return "", ErrNoMatchingResourceRule
	}
	if resourceType != ResourceTypeScript && resourceType != ResourceTypeStylesheet {
		return "", ErrNoMatchingResourceRule
	}

	identity, err := utils.CanonicalizeURLV1(rawURL)
	if err != nil {
		return "", fmt.Errorf("resource URL is invalid: %w", err)
	}
	if rawURL != identity.CanonicalURL {
		return "", fmt.Errorf("resource URL must already be canonical")
	}
	if err := utils.RequireStaticCrawlEligibility(identity); err != nil {
		return "", fmt.Errorf("resource URL is not statically eligible: %w", err)
	}

	parsed, err := url.Parse(identity.CanonicalURL)
	if err != nil {
		return "", fmt.Errorf("resource URL is invalid: %w", err)
	}
	if parsed.Scheme != "https" {
		return "", fmt.Errorf("resource URL must use HTTPS")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("resource URL must not contain userinfo")
	}
	if parsed.ForceQuery || parsed.RawQuery != "" {
		return "", fmt.Errorf("resource URL must not contain a query")
	}
	if parsed.Fragment != "" || parsed.RawFragment != "" {
		return "", fmt.Errorf("resource URL must not contain a fragment")
	}
	if parsed.Port() != "" {
		return "", fmt.Errorf("resource URL must not contain a non-default port")
	}
	if !utils.IsUnambiguousFetchPath(parsed) {
		return "", fmt.Errorf("resource URL has an ambiguous path")
	}

	host := parsed.Hostname()
	path := parsed.EscapedPath()
	allowed := false
	for _, resourceRule := range rule.ResourceRules {
		if resourceRule.Host != host || !containsResourceType(resourceRule.AllowedTypes, resourceType) {
			continue
		}
		if pathHasPrefix(path, resourceRule.DenyPathPrefixes) {
			return "", ErrNoMatchingResourceRule
		}
		if contains(resourceRule.AllowPaths, path) || pathHasPrefix(path, resourceRule.AllowPathPrefixes) {
			allowed = true
		}
	}
	if !allowed {
		return "", ErrNoMatchingResourceRule
	}
	return identity.CanonicalURL, nil
}

func pathHasPrefix(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsResourceType(values []ResourceType, target ResourceType) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
