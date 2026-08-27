// Package renderpolicy loads and matches the fail-closed JavaScript render
// policy. It authorizes rendering only; it can never authorize network access.
package renderpolicy

import (
	"errors"
	"time"
)

const SchemaVersionV1 = 1

const (
	ModeInlineOnly Mode = "inline_only"
	ModeBrokered   Mode = "brokered"
)

const (
	maxPolicyBytes = 1024 * 1024
	maxPolicyRules = 256
)

var (
	ErrNoMatchingRule         = errors.New("render policy has no matching enabled rule")
	ErrNoMatchingResourceRule = errors.New("render policy has no matching resource rule")
)

type Mode string

// ResourceType is a resource category that render-policy V1 can authorize.
type ResourceType string

const (
	ResourceTypeScript     ResourceType = "script"
	ResourceTypeStylesheet ResourceType = "stylesheet"
)

type Limits struct {
	MaxRenderTime             time.Duration
	SettleTime                time.Duration
	MaxResourceRequests       int
	MaxAggregateResourceBytes int64
	MaxResourceBodyBytes      int64
	MaxRenderedDOMBytes       int64
	MaxDOMNodes               int
	MaxRedirectHops           int
	MaxConsoleBytes           int64
}

// ResourceRule is a validated exact-host resource allowlist. Paths and path
// prefixes are canonical escaped paths and deny prefixes take precedence.
type ResourceRule struct {
	Host              string
	AllowPaths        []string
	AllowPathPrefixes []string
	DenyPathPrefixes  []string
	AllowedTypes      []ResourceType
}

type Rule struct {
	ID                string
	Enabled           bool
	Host              string
	AllowPaths        []string
	AllowPathPrefixes []string
	DenyPathPrefixes  []string
	Mode              Mode
	ResourceRules     []ResourceRule
	Limits            Limits
}

type Policy struct {
	rules []Rule
}

func (p *Policy) Rules() []Rule {
	if p == nil {
		return nil
	}
	rules := make([]Rule, len(p.rules))
	for index, rule := range p.rules {
		rules[index] = cloneRule(rule)
	}
	return rules
}

func (p *Policy) EnabledRuleCount() int {
	count := 0
	for _, rule := range p.rules {
		if rule.Enabled {
			count++
		}
	}
	return count
}

func cloneRule(rule Rule) Rule {
	rule.AllowPaths = append([]string(nil), rule.AllowPaths...)
	rule.AllowPathPrefixes = append([]string(nil), rule.AllowPathPrefixes...)
	rule.DenyPathPrefixes = append([]string(nil), rule.DenyPathPrefixes...)
	rule.ResourceRules = append([]ResourceRule(nil), rule.ResourceRules...)
	for index := range rule.ResourceRules {
		rule.ResourceRules[index].AllowPaths = append([]string(nil), rule.ResourceRules[index].AllowPaths...)
		rule.ResourceRules[index].AllowPathPrefixes = append([]string(nil), rule.ResourceRules[index].AllowPathPrefixes...)
		rule.ResourceRules[index].DenyPathPrefixes = append([]string(nil), rule.ResourceRules[index].DenyPathPrefixes...)
		rule.ResourceRules[index].AllowedTypes = append([]ResourceType(nil), rule.ResourceRules[index].AllowedTypes...)
	}
	return rule
}
