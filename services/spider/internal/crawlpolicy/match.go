package crawlpolicy

import (
	"net/url"
	"strings"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

// Match canonicalizes rawURL, enforces the repository's static crawl safety,
// deterministically selects a host rule, and applies the selected group. Host
// selection never uses JSON order or group scheduling priority: exact rules
// precede apex_and_subdomains rules, then the most-specific hostname wins.
//
// On a policy denial, Match returns a typed *DenialError. When canonicalization
// succeeded, the returned Decision is populated as far as selection reached.
func (p *Policy) Match(rawURL string, depth int) (Decision, error) {
	return p.match(rawURL, depth, true)
}

// MatchRobots applies domain, static-safety, enabled-group, and scheme rules
// while deliberately bypassing page path and depth scope for /robots.txt.
// The robots fetch remains subject to DNS, redirect, and request-budget checks.
func (p *Policy) MatchRobots(rawURL string) (Decision, error) {
	decision, err := p.match(rawURL, 0, false)
	if err != nil {
		return decision, err
	}
	if decision.Path != "/robots.txt" || decision.URL == nil || decision.URL.ForceQuery || decision.URL.RawQuery != "" {
		return decision, &DenialError{Reason: ReasonPathDenied}
	}
	return decision, nil
}

func (p *Policy) match(rawURL string, depth int, enforcePageScope bool) (Decision, error) {
	identity, err := utils.CanonicalizeURLV1(rawURL)
	if err != nil {
		return Decision{}, &DenialError{Reason: ReasonInvalidURL, Cause: err}
	}

	parsed, err := url.Parse(identity.CanonicalURL)
	if err != nil {
		// CanonicalizeURLV1 emits a parseable absolute URL. Keep this defensive
		// boundary typed in case that invariant changes in a future dependency.
		return Decision{Identity: identity}, &DenialError{Reason: ReasonInvalidURL, Cause: err}
	}
	decision := Decision{
		Identity: identity,
		URL:      parsed,
		Scheme:   parsed.Scheme,
		Host:     parsed.Hostname(),
		Path:     parsed.EscapedPath(),
	}
	if !utils.IsUnambiguousFetchPath(parsed) {
		return decision, &DenialError{Reason: ReasonAmbiguousPath}
	}

	if err := utils.RequireStaticCrawlEligibility(identity); err != nil {
		return decision, &DenialError{Reason: ReasonStaticSafety, Cause: err}
	}
	if p == nil {
		return decision, &DenialError{Reason: ReasonUnknownDomain}
	}

	var selected *compiledHostRule
	for index := range p.rules {
		if hostMatchesRule(decision.Host, p.rules[index].rule) {
			selected = &p.rules[index]
			break
		}
	}
	if selected == nil {
		return decision, &DenialError{Reason: ReasonUnknownDomain}
	}

	group := p.groupsByID[selected.groupID]
	decision.Group = cloneGroup(group)
	decision.MatchedHostRule = selected.rule

	if !group.Enabled {
		return decision, &DenialError{Reason: ReasonGroupDisabled}
	}
	if !containsString(group.AllowedSchemes, decision.Scheme) {
		return decision, &DenialError{Reason: ReasonSchemeNotAllowed}
	}
	if enforcePageScope {
		if pathHasPrefix(decision.Path, group.DenyPathPrefixes) {
			return decision, &DenialError{Reason: ReasonPathDenied}
		}
		if len(group.AllowPathPrefixes) > 0 && !pathHasPrefix(decision.Path, group.AllowPathPrefixes) {
			return decision, &DenialError{Reason: ReasonPathNotAllowed}
		}
		if depth < 0 {
			return decision, &DenialError{Reason: ReasonInvalidDepth}
		}
		if depth > group.MaxDepth {
			return decision, &DenialError{Reason: ReasonDepthExceeded}
		}
	}

	return decision, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func pathHasPrefix(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
