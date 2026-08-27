package crawlpolicy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

type policyDocument struct {
	SchemaVersion   *int             `json:"schema_version"`
	UnmatchedAction *UnmatchedAction `json:"unmatched_action"`
	Groups          *[]groupDocument `json:"groups"`
}

type groupDocument struct {
	ID                  *string             `json:"id"`
	Enabled             *bool               `json:"enabled"`
	Priority            *int                `json:"priority"`
	HostRules           *[]hostRuleDocument `json:"host_rules"`
	AllowedSchemes      *[]string           `json:"allowed_schemes"`
	MaxDepth            *int                `json:"max_depth"`
	AllowPathPrefixes   *[]string           `json:"allow_path_prefixes"`
	DenyPathPrefixes    *[]string           `json:"deny_path_prefixes"`
	MinRequestInterval  *string             `json:"min_request_interval"`
	MaxConcurrency      *int                `json:"max_concurrency"`
	MaxRequestsPerBatch *int                `json:"max_requests_per_batch"`
	UserAgent           optionalString      `json:"user_agent"`
	Redirects           *redirectDocument   `json:"redirects"`
	Robots              *robotsDocument     `json:"robots"`
}

type hostRuleDocument struct {
	Host  *string    `json:"host"`
	Match *HostMatch `json:"match"`
}

type redirectDocument struct {
	Mode    *RedirectMode `json:"mode"`
	MaxHops *int          `json:"max_hops"`
}

type robotsDocument struct {
	Mode     *RobotsMode        `json:"mode"`
	OnError  *RobotsErrorAction `json:"on_error"`
	CacheTTL *string            `json:"cache_ttl"`
}

// optionalString distinguishes an omitted optional string from JSON null.
type optionalString struct {
	present bool
	value   string
}

func (value *optionalString) UnmarshalJSON(data []byte) error {
	value.present = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return fmt.Errorf("must be a string, not null")
	}
	return json.Unmarshal(data, &value.value)
}

// ValidationError identifies a semantically invalid field after strict JSON
// decoding. Field uses a JSON-path-like notation suitable for diagnostics.
type ValidationError struct {
	Field   string
	Problem string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid crawl policy field %s: %s", e.Field, e.Problem)
}

func invalidField(field, format string, arguments ...any) error {
	return &ValidationError{Field: field, Problem: fmt.Sprintf(format, arguments...)}
}

// Load opens path and strictly decodes a V1 policy. fallbackUserAgent is used
// by every group that omits user_agent. Loading performs no network access.
func Load(path, fallbackUserAgent string) (*Policy, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open crawl policy %q: %w", path, err)
	}
	defer file.Close()

	policy, err := Decode(file, fallbackUserAgent)
	if err != nil {
		return nil, fmt.Errorf("decode crawl policy %q: %w", path, err)
	}
	return policy, nil
}

// Decode strictly decodes and validates one V1 JSON document. Unknown fields,
// duplicate object members, invalid UTF-8, and any trailing JSON are rejected.
// Per-group user agents fall back to fallbackUserAgent after control-character
// validation.
func Decode(reader io.Reader, fallbackUserAgent string) (*Policy, error) {
	if reader == nil {
		return nil, fmt.Errorf("decode crawl policy: nil reader")
	}
	if err := validateUserAgent(fallbackUserAgent); err != nil {
		return nil, invalidField("fallback_user_agent", "%v", err)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read crawl policy: %w", err)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("decode crawl policy: invalid UTF-8")
	}
	if err := validateSingleJSONDocument(data); err != nil {
		return nil, fmt.Errorf("decode crawl policy: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document policyDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode crawl policy: %w", err)
	}

	return validateDocument(document, fallbackUserAgent)
}

func validateSingleJSONDocument(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder, "$"); err != nil {
		return err
	}

	if token, err := decoder.Token(); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("trailing JSON: %w", err)
	} else {
		return fmt.Errorf("trailing JSON value beginning with %v", token)
	}
}

func consumeJSONValue(decoder *json.Decoder, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}

	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object member at %s is not a string", path)
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate object member %q at %s", key, path)
			}
			seen[key] = struct{}{}
			if err := consumeJSONValue(decoder, path+"."+key); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return fmt.Errorf("object at %s is not closed", path)
		}
		return nil
	case '[':
		for index := 0; decoder.More(); index++ {
			if err := consumeJSONValue(decoder, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return fmt.Errorf("array at %s is not closed", path)
		}
		return nil
	default:
		return fmt.Errorf("unexpected JSON delimiter %q at %s", delimiter, path)
	}
}

func validateDocument(document policyDocument, fallbackUserAgent string) (*Policy, error) {
	if document.SchemaVersion == nil {
		return nil, invalidField("schema_version", "is required and must not be null")
	}
	if *document.SchemaVersion != SchemaVersionV1 {
		return nil, invalidField("schema_version", "must equal %d", SchemaVersionV1)
	}
	if document.UnmatchedAction == nil {
		return nil, invalidField("unmatched_action", "is required and must not be null")
	}
	if *document.UnmatchedAction != UnmatchedDeny {
		return nil, invalidField("unmatched_action", "V1 supports only %q", UnmatchedDeny)
	}
	if document.Groups == nil {
		return nil, invalidField("groups", "is required and must not be null")
	}

	policy := &Policy{
		schemaVersion:   SchemaVersionV1,
		unmatchedAction: UnmatchedDeny,
		groupsByID:      make(map[string]Group, len(*document.Groups)),
	}
	ruleKeys := make(map[string]string)

	for index, groupDocument := range *document.Groups {
		field := fmt.Sprintf("groups[%d]", index)
		group, err := validateGroup(groupDocument, field, fallbackUserAgent)
		if err != nil {
			return nil, err
		}
		if _, duplicate := policy.groupsByID[group.ID]; duplicate {
			return nil, invalidField(field+".id", "duplicate group ID %q", group.ID)
		}

		for _, rule := range group.HostRules {
			key := string(rule.Match) + "\x00" + rule.Host
			if owner, duplicate := ruleKeys[key]; duplicate {
				return nil, invalidField(field+".host_rules", "duplicate host rule %s/%s (already in group %q)", rule.Host, rule.Match, owner)
			}
			ruleKeys[key] = group.ID

			compiled := compiledHostRule{
				rule:        rule,
				groupID:     group.ID,
				labelCount:  hostnameLabelCount(rule.Host),
				matchWeight: hostMatchWeight(rule.Match),
			}
			for _, existing := range policy.rules {
				if existing.groupID == compiled.groupID || existing.labelCount != compiled.labelCount {
					continue
				}
				if hostRulesOverlap(existing.rule, compiled.rule) {
					return nil, invalidField(field+".host_rules", "equal-specificity rules %s/%s and %s/%s overlap across groups %q and %q", existing.rule.Host, existing.rule.Match, compiled.rule.Host, compiled.rule.Match, existing.groupID, compiled.groupID)
				}
			}
			policy.rules = append(policy.rules, compiled)
		}

		policy.groupsByID[group.ID] = group
		policy.orderedGroupIDs = append(policy.orderedGroupIDs, group.ID)
	}

	sort.Slice(policy.orderedGroupIDs, func(left, right int) bool {
		leftGroup := policy.groupsByID[policy.orderedGroupIDs[left]]
		rightGroup := policy.groupsByID[policy.orderedGroupIDs[right]]
		if leftGroup.Priority != rightGroup.Priority {
			return leftGroup.Priority < rightGroup.Priority
		}
		return leftGroup.ID < rightGroup.ID
	})
	sort.Slice(policy.rules, func(left, right int) bool {
		return compiledRuleLess(policy.rules[left], policy.rules[right])
	})

	return policy, nil
}

func validateGroup(document groupDocument, field, fallbackUserAgent string) (Group, error) {
	if document.ID == nil {
		return Group{}, invalidField(field+".id", "is required and must not be null")
	}
	if *document.ID == "" {
		return Group{}, invalidField(field+".id", "must not be empty")
	}
	if !utf8.ValidString(*document.ID) || containsControl(*document.ID) {
		return Group{}, invalidField(field+".id", "must be valid UTF-8 without control characters")
	}
	if document.Enabled == nil {
		return Group{}, invalidField(field+".enabled", "is required and must not be null")
	}
	if document.Priority == nil {
		return Group{}, invalidField(field+".priority", "is required and must not be null")
	}
	if document.HostRules == nil {
		return Group{}, invalidField(field+".host_rules", "is required and must not be null")
	}
	if len(*document.HostRules) == 0 {
		return Group{}, invalidField(field+".host_rules", "must contain at least one rule")
	}
	if document.AllowedSchemes == nil {
		return Group{}, invalidField(field+".allowed_schemes", "is required and must not be null")
	}
	if len(*document.AllowedSchemes) == 0 {
		return Group{}, invalidField(field+".allowed_schemes", "must contain at least one scheme")
	}
	if document.MaxDepth == nil {
		return Group{}, invalidField(field+".max_depth", "is required and must not be null")
	}
	if *document.MaxDepth < 0 || uint64(*document.MaxDepth) > utils.MaxCrawlDepthV1 {
		return Group{}, invalidField(field+".max_depth", "must be between 0 and 9007199254740991")
	}
	if document.AllowPathPrefixes == nil {
		return Group{}, invalidField(field+".allow_path_prefixes", "is required and must not be null")
	}
	if document.DenyPathPrefixes == nil {
		return Group{}, invalidField(field+".deny_path_prefixes", "is required and must not be null")
	}
	if document.MinRequestInterval == nil {
		return Group{}, invalidField(field+".min_request_interval", "is required and must not be null")
	}
	minimumInterval, err := time.ParseDuration(*document.MinRequestInterval)
	if err != nil || minimumInterval < 0 {
		return Group{}, invalidField(field+".min_request_interval", "must be a non-negative Go duration")
	}
	if document.MaxConcurrency == nil {
		return Group{}, invalidField(field+".max_concurrency", "is required and must not be null")
	}
	if *document.MaxConcurrency <= 0 {
		return Group{}, invalidField(field+".max_concurrency", "must be greater than 0")
	}
	if document.MaxRequestsPerBatch == nil {
		return Group{}, invalidField(field+".max_requests_per_batch", "is required and must not be null")
	}
	if *document.MaxRequestsPerBatch <= 0 {
		return Group{}, invalidField(field+".max_requests_per_batch", "must be greater than 0")
	}
	if document.Redirects == nil {
		return Group{}, invalidField(field+".redirects", "is required and must not be null")
	}
	redirects, err := validateRedirects(*document.Redirects, field+".redirects")
	if err != nil {
		return Group{}, err
	}
	if document.Robots == nil {
		return Group{}, invalidField(field+".robots", "is required and must not be null")
	}
	robots, err := validateRobots(*document.Robots, field+".robots")
	if err != nil {
		return Group{}, err
	}

	hostRules := make([]HostRule, 0, len(*document.HostRules))
	for index, ruleDocument := range *document.HostRules {
		rule, err := validateHostRule(ruleDocument, fmt.Sprintf("%s.host_rules[%d]", field, index))
		if err != nil {
			return Group{}, err
		}
		hostRules = append(hostRules, rule)
	}
	sort.Slice(hostRules, func(left, right int) bool {
		leftWeight := hostMatchWeight(hostRules[left].Match)
		rightWeight := hostMatchWeight(hostRules[right].Match)
		if leftWeight != rightWeight {
			return leftWeight > rightWeight
		}
		return hostRules[left].Host < hostRules[right].Host
	})

	allowedSchemes := append([]string(nil), (*document.AllowedSchemes)...)
	seenSchemes := make(map[string]struct{}, len(allowedSchemes))
	for index, scheme := range allowedSchemes {
		if scheme != "http" && scheme != "https" {
			return Group{}, invalidField(fmt.Sprintf("%s.allowed_schemes[%d]", field, index), "must be either %q or %q", "http", "https")
		}
		if _, duplicate := seenSchemes[scheme]; duplicate {
			return Group{}, invalidField(fmt.Sprintf("%s.allowed_schemes[%d]", field, index), "duplicate scheme %q", scheme)
		}
		seenSchemes[scheme] = struct{}{}
	}
	sort.Strings(allowedSchemes)

	allowPrefixes, err := validatePathPrefixes(*document.AllowPathPrefixes, field+".allow_path_prefixes")
	if err != nil {
		return Group{}, err
	}
	denyPrefixes, err := validatePathPrefixes(*document.DenyPathPrefixes, field+".deny_path_prefixes")
	if err != nil {
		return Group{}, err
	}

	userAgent := fallbackUserAgent
	if document.UserAgent.present {
		userAgent = document.UserAgent.value
	}
	if err := validateUserAgent(userAgent); err != nil {
		return Group{}, invalidField(field+".user_agent", "%v", err)
	}

	return Group{
		ID:                  *document.ID,
		Enabled:             *document.Enabled,
		Priority:            *document.Priority,
		HostRules:           hostRules,
		AllowedSchemes:      allowedSchemes,
		MaxDepth:            *document.MaxDepth,
		AllowPathPrefixes:   allowPrefixes,
		DenyPathPrefixes:    denyPrefixes,
		MinRequestInterval:  minimumInterval,
		MaxConcurrency:      *document.MaxConcurrency,
		MaxRequestsPerBatch: *document.MaxRequestsPerBatch,
		UserAgent:           userAgent,
		Redirects:           redirects,
		Robots:              robots,
	}, nil
}

func validateHostRule(document hostRuleDocument, field string) (HostRule, error) {
	if document.Host == nil {
		return HostRule{}, invalidField(field+".host", "is required and must not be null")
	}
	if document.Match == nil {
		return HostRule{}, invalidField(field+".match", "is required and must not be null")
	}
	if *document.Match != MatchExact && *document.Match != MatchApexAndSubdomains {
		return HostRule{}, invalidField(field+".match", "must be %q or %q", MatchExact, MatchApexAndSubdomains)
	}

	host := *document.Host
	if host == "" || !utf8.ValidString(host) {
		return HostRule{}, invalidField(field+".host", "must be a non-empty UTF-8 hostname")
	}
	if host != strings.ToLower(host) {
		return HostRule{}, invalidField(field+".host", "must be lowercase canonical ASCII")
	}
	for _, character := range host {
		if character > unicode.MaxASCII {
			return HostRule{}, invalidField(field+".host", "must be canonical ASCII (use a valid lowercase IDNA A-label where needed)")
		}
	}
	if strings.ContainsAny(host, "*/\\:@/?#") {
		return HostRule{}, invalidField(field+".host", "must not contain a wildcard, scheme, path, port, or URL authority syntax")
	}

	identity, err := utils.CanonicalizeURLV1("https://" + host + "/")
	if err != nil {
		return HostRule{}, invalidField(field+".host", "is not a canonical DNS hostname: %v", err)
	}
	if identity.CanonicalURL != "https://"+host+"/" {
		return HostRule{}, invalidField(field+".host", "must already be in lowercase canonical DNS form")
	}
	if err := utils.RequireStaticCrawlEligibility(identity); err != nil {
		return HostRule{}, invalidField(field+".host", "is forbidden by static crawl safety: %v", err)
	}

	return HostRule{Host: host, Match: *document.Match}, nil
}

func validatePathPrefixes(prefixes []string, field string) ([]string, error) {
	validated := append([]string(nil), prefixes...)
	seen := make(map[string]struct{}, len(validated))
	for index, prefix := range validated {
		itemField := fmt.Sprintf("%s[%d]", field, index)
		if prefix == "" || !strings.HasPrefix(prefix, "/") {
			return nil, invalidField(itemField, "must be a non-empty absolute path beginning with /")
		}
		if strings.ContainsAny(prefix, "?#\\") {
			return nil, invalidField(itemField, "must contain only a path, not a query, fragment, or backslash")
		}
		identity, err := utils.CanonicalizeURLV1("https://example.com" + prefix)
		if err != nil {
			return nil, invalidField(itemField, "is not a valid canonical escaped path: %v", err)
		}
		if identity.CanonicalURL != "https://example.com"+prefix {
			return nil, invalidField(itemField, "must already be in canonical escaped-path form")
		}
		parsed, parseErr := url.Parse(identity.CanonicalURL)
		if parseErr != nil || !utils.IsUnambiguousFetchPath(parsed) {
			return nil, invalidField(itemField, "must be an unambiguous fetch path")
		}
		if _, duplicate := seen[prefix]; duplicate {
			return nil, invalidField(itemField, "duplicate path prefix %q", prefix)
		}
		seen[prefix] = struct{}{}
	}
	return validated, nil
}

func validateRedirects(document redirectDocument, field string) (RedirectPolicy, error) {
	if document.Mode == nil {
		return RedirectPolicy{}, invalidField(field+".mode", "is required and must not be null")
	}
	switch *document.Mode {
	case RedirectNone, RedirectSameHost, RedirectSameGroup:
	default:
		return RedirectPolicy{}, invalidField(field+".mode", "must be %q, %q, or %q", RedirectNone, RedirectSameHost, RedirectSameGroup)
	}
	if document.MaxHops == nil {
		return RedirectPolicy{}, invalidField(field+".max_hops", "is required and must not be null")
	}
	if *document.MaxHops < 0 || *document.MaxHops > 10 {
		return RedirectPolicy{}, invalidField(field+".max_hops", "must be between 0 and 10")
	}
	return RedirectPolicy{Mode: *document.Mode, MaxHops: *document.MaxHops}, nil
}

func validateRobots(document robotsDocument, field string) (RobotsPolicy, error) {
	if document.Mode == nil {
		return RobotsPolicy{}, invalidField(field+".mode", "is required and must not be null")
	}
	if *document.Mode != RobotsEnforce {
		return RobotsPolicy{}, invalidField(field+".mode", "V1 supports only %q", RobotsEnforce)
	}
	if document.OnError == nil {
		return RobotsPolicy{}, invalidField(field+".on_error", "is required and must not be null")
	}
	if *document.OnError != RobotsErrorAllow && *document.OnError != RobotsErrorDeny {
		return RobotsPolicy{}, invalidField(field+".on_error", "must be %q or %q", RobotsErrorAllow, RobotsErrorDeny)
	}
	if document.CacheTTL == nil {
		return RobotsPolicy{}, invalidField(field+".cache_ttl", "is required and must not be null")
	}
	cacheTTL, err := time.ParseDuration(*document.CacheTTL)
	if err != nil || cacheTTL <= 0 {
		return RobotsPolicy{}, invalidField(field+".cache_ttl", "must be a positive Go duration")
	}
	return RobotsPolicy{Mode: *document.Mode, OnError: *document.OnError, CacheTTL: cacheTTL}, nil
}

func validateUserAgent(userAgent string) error {
	if !utf8.ValidString(userAgent) {
		return fmt.Errorf("must be valid UTF-8")
	}
	if userAgent == "" {
		return fmt.Errorf("must not be empty")
	}
	if strings.TrimSpace(userAgent) != userAgent {
		return fmt.Errorf("must not contain leading or trailing whitespace")
	}
	if containsControl(userAgent) {
		return fmt.Errorf("must not contain control characters")
	}
	return nil
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func hostnameLabelCount(host string) int {
	return strings.Count(host, ".") + 1
}

func hostMatchWeight(match HostMatch) int {
	if match == MatchExact {
		return 2
	}
	return 1
}

func compiledRuleLess(left, right compiledHostRule) bool {
	if left.matchWeight != right.matchWeight {
		return left.matchWeight > right.matchWeight
	}
	if left.labelCount != right.labelCount {
		return left.labelCount > right.labelCount
	}
	if len(left.rule.Host) != len(right.rule.Host) {
		return len(left.rule.Host) > len(right.rule.Host)
	}
	if left.rule.Host != right.rule.Host {
		return left.rule.Host < right.rule.Host
	}
	return left.groupID < right.groupID
}

func hostRulesOverlap(left, right HostRule) bool {
	switch {
	case left.Match == MatchExact && right.Match == MatchExact:
		return left.Host == right.Host
	case left.Match == MatchExact:
		return hostMatchesRule(left.Host, right)
	case right.Match == MatchExact:
		return hostMatchesRule(right.Host, left)
	default:
		return hostMatchesRule(left.Host, right) || hostMatchesRule(right.Host, left)
	}
}

func hostMatchesRule(host string, rule HostRule) bool {
	if host == rule.Host {
		return true
	}
	return rule.Match == MatchApexAndSubdomains && strings.HasSuffix(host, "."+rule.Host)
}
