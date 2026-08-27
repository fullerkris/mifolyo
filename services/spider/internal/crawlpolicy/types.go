// Package crawlpolicy loads and enforces the version 1 domain-group crawl
// policy contract. Policy matching is local and deterministic; it never does
// DNS resolution or makes a network request.
package crawlpolicy

import (
	"net/url"
	"time"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

const (
	// SchemaVersionV1 is the only policy schema version accepted by Decode.
	SchemaVersionV1 = 1
)

// UnmatchedAction is the action applied when no host rule matches.
type UnmatchedAction string

const (
	// UnmatchedDeny is the only unmatched action supported by V1.
	UnmatchedDeny UnmatchedAction = "deny"
)

// HostMatch describes how a canonical hostname is compared with a host rule.
type HostMatch string

const (
	// MatchExact matches only the rule's hostname.
	MatchExact HostMatch = "exact"
	// MatchApexAndSubdomains matches the hostname and its DNS-label-bounded
	// subdomains.
	MatchApexAndSubdomains HostMatch = "apex_and_subdomains"
)

// RedirectMode controls which redirect targets an integrating crawler may
// follow. Redirect hops still need to be passed through Policy.Match.
type RedirectMode string

const (
	RedirectNone      RedirectMode = "none"
	RedirectSameHost  RedirectMode = "same_host"
	RedirectSameGroup RedirectMode = "same_group"
)

// RobotsMode controls robots.txt enforcement.
type RobotsMode string

const (
	// RobotsEnforce is the only robots mode supported by V1.
	RobotsEnforce RobotsMode = "enforce"
)

// RobotsErrorAction controls the fail-open/fail-closed behavior when robots.txt
// cannot be obtained or parsed.
type RobotsErrorAction string

const (
	RobotsErrorAllow RobotsErrorAction = "allow"
	RobotsErrorDeny  RobotsErrorAction = "deny"
)

// HostRule is a validated canonical DNS hostname rule.
type HostRule struct {
	Host  string
	Match HostMatch
}

// RedirectPolicy contains the V1 redirect boundary and hop limit.
type RedirectPolicy struct {
	Mode    RedirectMode
	MaxHops int
}

// RobotsPolicy contains the required V1 robots behavior.
type RobotsPolicy struct {
	Mode     RobotsMode
	OnError  RobotsErrorAction
	CacheTTL time.Duration
}

// Group is a validated crawl group. UserAgent is always resolved: it contains
// the group's user_agent when configured and otherwise the fallback supplied
// to Load or Decode. Path prefixes are matched against the canonical escaped
// URL path using literal prefix matching.
type Group struct {
	ID                  string
	Enabled             bool
	Priority            int
	HostRules           []HostRule
	AllowedSchemes      []string
	MaxDepth            int
	AllowPathPrefixes   []string
	DenyPathPrefixes    []string
	MinRequestInterval  time.Duration
	MaxConcurrency      int
	MaxRequestsPerBatch int
	UserAgent           string
	Redirects           RedirectPolicy
	Robots              RobotsPolicy
}

// Decision is the canonical, selected policy identity for a URL. Match also
// returns a partially populated Decision with a denial error whenever
// canonicalization succeeded. Group and MatchedHostRule are populated after a
// host rule has been selected.
type Decision struct {
	Identity        utils.CanonicalizedURL
	URL             *url.URL
	Scheme          string
	Host            string
	Path            string
	Group           Group
	MatchedHostRule HostRule
}

// Policy is an immutable, validated V1 policy. Its accessors return copies so
// callers cannot alter matching or runtime limits after validation.
type Policy struct {
	schemaVersion   int
	unmatchedAction UnmatchedAction
	groupsByID      map[string]Group
	orderedGroupIDs []string
	rules           []compiledHostRule
}

type compiledHostRule struct {
	rule        HostRule
	groupID     string
	labelCount  int
	matchWeight int
}

// SchemaVersion returns the decoded contract version (always 1).
func (p *Policy) SchemaVersion() int {
	if p == nil {
		return 0
	}
	return p.schemaVersion
}

// UnmatchedAction returns the policy's unmatched action (always deny in V1).
func (p *Policy) UnmatchedAction() UnmatchedAction {
	if p == nil {
		return ""
	}
	return p.unmatchedAction
}

// Group returns a defensive copy of the named group.
func (p *Policy) Group(id string) (Group, bool) {
	if p == nil {
		return Group{}, false
	}
	group, ok := p.groupsByID[id]
	if !ok {
		return Group{}, false
	}
	return cloneGroup(group), true
}

// Groups returns defensive copies in deterministic scheduling order: lower
// priority first, then lexicographically by group ID. This ordering is not used
// by URL matching.
func (p *Policy) Groups() []Group {
	if p == nil {
		return nil
	}
	groups := make([]Group, 0, len(p.orderedGroupIDs))
	for _, id := range p.orderedGroupIDs {
		groups = append(groups, cloneGroup(p.groupsByID[id]))
	}
	return groups
}

func cloneGroup(group Group) Group {
	group.HostRules = append([]HostRule(nil), group.HostRules...)
	group.AllowedSchemes = append([]string(nil), group.AllowedSchemes...)
	group.AllowPathPrefixes = append([]string(nil), group.AllowPathPrefixes...)
	group.DenyPathPrefixes = append([]string(nil), group.DenyPathPrefixes...)
	return group
}
