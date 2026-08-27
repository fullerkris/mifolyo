package renderpolicy

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/IonelPopJara/search-engine/services/spider/internal/strictjson"
	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

var hostnamePattern = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

type policyDocument struct {
	SchemaVersion *int            `json:"schema_version"`
	DefaultAction *string         `json:"default_action"`
	Rules         *[]ruleDocument `json:"rules"`
}

type ruleDocument struct {
	ID                *string                  `json:"id"`
	Enabled           *bool                    `json:"enabled"`
	HostRule          *hostRuleDocument        `json:"host_rule"`
	AllowPaths        *[]string                `json:"allow_paths"`
	AllowPathPrefixes *[]string                `json:"allow_path_prefixes"`
	DenyPathPrefixes  *[]string                `json:"deny_path_prefixes"`
	Mode              *Mode                    `json:"mode"`
	FailureAction     *string                  `json:"failure_action"`
	ResourceRules     *[]resourceRuleDocument  `json:"resource_rules"`
	NetworkControls   *networkControlsDocument `json:"network_controls"`
	Limits            *limitsDocument          `json:"limits"`
}

type hostRuleDocument struct {
	Host  *string `json:"host"`
	Match *string `json:"match"`
}

type resourceRuleDocument struct {
	HostRule          *hostRuleDocument `json:"host_rule"`
	AllowPaths        *[]string         `json:"allow_paths"`
	AllowPathPrefixes *[]string         `json:"allow_path_prefixes"`
	DenyPathPrefixes  *[]string         `json:"deny_path_prefixes"`
	AllowedTypes      *[]ResourceType   `json:"allowed_types"`
}

type networkControlsDocument struct {
	AllowedMethods            *[]string `json:"allowed_methods"`
	RobotsForResources        *bool     `json:"robots_for_resources"`
	AllowCookies              *bool     `json:"allow_cookies"`
	AllowServiceWorkers       *bool     `json:"allow_service_workers"`
	AllowWebSockets           *bool     `json:"allow_websockets"`
	AllowWebRTC               *bool     `json:"allow_webrtc"`
	AllowDownloads            *bool     `json:"allow_downloads"`
	AllowPopups               *bool     `json:"allow_popups"`
	AllowSecondaryDocuments   *bool     `json:"allow_secondary_documents"`
	AllowJavaScriptNavigation *bool     `json:"allow_javascript_navigation"`
}

type limitsDocument struct {
	MaxRenderTimeMS           *int   `json:"max_render_time_ms"`
	SettleTimeMS              *int   `json:"settle_time_ms"`
	MaxResourceRequests       *int   `json:"max_resource_requests"`
	MaxAggregateResourceBytes *int64 `json:"max_aggregate_resource_bytes"`
	MaxResourceBodyBytes      *int64 `json:"max_resource_body_bytes"`
	MaxRenderedDOMBytes       *int64 `json:"max_rendered_dom_bytes"`
	MaxDOMNodes               *int   `json:"max_dom_nodes"`
	MaxRedirectHops           *int   `json:"max_redirect_hops"`
	MaxConsoleBytes           *int64 `json:"max_console_bytes"`
}

func Load(path string) (*Policy, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open render policy %q: %w", path, err)
	}
	defer file.Close()
	policy, err := Decode(file)
	if err != nil {
		return nil, fmt.Errorf("decode render policy %q: %w", path, err)
	}
	return policy, nil
}

func Decode(reader io.Reader) (*Policy, error) {
	if reader == nil {
		return nil, fmt.Errorf("decode render policy: nil reader")
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxPolicyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read render policy: %w", err)
	}
	if len(data) > maxPolicyBytes {
		return nil, fmt.Errorf("render policy exceeds %d bytes", maxPolicyBytes)
	}
	var document policyDocument
	if err := strictjson.Decode(data, &document); err != nil {
		return nil, fmt.Errorf("decode render policy: %w", err)
	}
	return validateDocument(document)
}

func validateDocument(document policyDocument) (*Policy, error) {
	if document.SchemaVersion == nil || *document.SchemaVersion != SchemaVersionV1 {
		return nil, invalid("schema_version", "must be present and equal %d", SchemaVersionV1)
	}
	if document.DefaultAction == nil || *document.DefaultAction != "deny" {
		return nil, invalid("default_action", "must be present and equal deny")
	}
	if document.Rules == nil {
		return nil, invalid("rules", "is required and must not be null")
	}
	if len(*document.Rules) > maxPolicyRules {
		return nil, invalid("rules", "must contain at most %d entries", maxPolicyRules)
	}

	policy := &Policy{rules: make([]Rule, 0, len(*document.Rules))}
	ids := make(map[string]struct{}, len(*document.Rules))
	hosts := make(map[string]string, len(*document.Rules))
	for index, ruleDocument := range *document.Rules {
		field := fmt.Sprintf("rules[%d]", index)
		rule, err := validateRule(ruleDocument, field)
		if err != nil {
			return nil, err
		}
		if _, exists := ids[rule.ID]; exists {
			return nil, invalid(field+".id", "duplicate rule ID %q", rule.ID)
		}
		ids[rule.ID] = struct{}{}
		if owner, exists := hosts[rule.Host]; exists {
			return nil, invalid(field+".host_rule.host", "host %q overlaps rule %q", rule.Host, owner)
		}
		hosts[rule.Host] = rule.ID
		policy.rules = append(policy.rules, rule)
	}
	sort.Slice(policy.rules, func(left, right int) bool {
		if policy.rules[left].Host != policy.rules[right].Host {
			return policy.rules[left].Host < policy.rules[right].Host
		}
		return policy.rules[left].ID < policy.rules[right].ID
	})
	return policy, nil
}

func validateRule(document ruleDocument, field string) (Rule, error) {
	if document.ID == nil || *document.ID == "" || !utf8.ValidString(*document.ID) || containsControl(*document.ID) {
		return Rule{}, invalid(field+".id", "must be a non-empty string without control characters")
	}
	if document.Enabled == nil {
		return Rule{}, invalid(field+".enabled", "is required and must not be null")
	}
	host, err := validateHostRule(document.HostRule, field+".host_rule")
	if err != nil {
		return Rule{}, err
	}
	allowPaths, err := validatePaths(document.AllowPaths, field+".allow_paths", true)
	if err != nil {
		return Rule{}, err
	}
	allowPrefixes, err := validatePaths(document.AllowPathPrefixes, field+".allow_path_prefixes", false)
	if err != nil {
		return Rule{}, err
	}
	denyPrefixes, err := validatePaths(document.DenyPathPrefixes, field+".deny_path_prefixes", true)
	if err != nil {
		return Rule{}, err
	}
	if len(allowPaths) == 0 && len(allowPrefixes) == 0 {
		return Rule{}, invalid(field, "must include an exact path or non-root path prefix")
	}
	if document.Mode == nil || (*document.Mode != ModeInlineOnly && *document.Mode != ModeBrokered) {
		return Rule{}, invalid(field+".mode", "must be inline_only or brokered")
	}
	if document.FailureAction == nil || *document.FailureAction != "reject_page" {
		return Rule{}, invalid(field+".failure_action", "must equal reject_page")
	}
	if document.ResourceRules == nil {
		return Rule{}, invalid(field+".resource_rules", "is required and must not be null")
	}
	if err := validateNetworkControls(document.NetworkControls, field+".network_controls"); err != nil {
		return Rule{}, err
	}
	limits, err := validateLimits(document.Limits, field+".limits")
	if err != nil {
		return Rule{}, err
	}
	if *document.Mode == ModeInlineOnly {
		if len(*document.ResourceRules) != 0 {
			return Rule{}, invalid(field+".resource_rules", "inline_only rules cannot authorize resources")
		}
		if limits.MaxResourceRequests != 0 || limits.MaxAggregateResourceBytes != 0 ||
			limits.MaxResourceBodyBytes != 0 || limits.MaxRedirectHops != 0 {
			return Rule{}, invalid(field+".limits", "inline_only resource limits must all be zero")
		}
	} else {
		if len(*document.ResourceRules) == 0 {
			return Rule{}, invalid(field+".resource_rules", "brokered rules require at least one resource rule")
		}
		if limits.MaxResourceRequests == 0 || limits.MaxAggregateResourceBytes == 0 || limits.MaxResourceBodyBytes == 0 {
			return Rule{}, invalid(field+".limits", "brokered resource limits must be positive")
		}
		if limits.MaxRedirectHops != 0 {
			return Rule{}, invalid(field+".limits.max_redirect_hops", "brokered resource redirects are not supported")
		}
	}
	resourceRules := make([]ResourceRule, 0, len(*document.ResourceRules))
	resourceKeys := make(map[string]struct{}, len(*document.ResourceRules))
	for index, resource := range *document.ResourceRules {
		resourceField := fmt.Sprintf("%s.resource_rules[%d]", field, index)
		resourceRule, key, err := validateResourceRule(resource, resourceField)
		if err != nil {
			return Rule{}, err
		}
		if _, duplicate := resourceKeys[key]; duplicate {
			return Rule{}, invalid(resourceField, "duplicates another resource rule")
		}
		resourceKeys[key] = struct{}{}
		resourceRules = append(resourceRules, resourceRule)
	}

	return Rule{
		ID:                *document.ID,
		Enabled:           *document.Enabled,
		Host:              host,
		AllowPaths:        allowPaths,
		AllowPathPrefixes: allowPrefixes,
		DenyPathPrefixes:  denyPrefixes,
		Mode:              *document.Mode,
		ResourceRules:     resourceRules,
		Limits:            limits,
	}, nil
}

func validateResourceRule(document resourceRuleDocument, field string) (ResourceRule, string, error) {
	host, err := validateHostRule(document.HostRule, field+".host_rule")
	if err != nil {
		return ResourceRule{}, "", err
	}
	paths, err := validatePaths(document.AllowPaths, field+".allow_paths", true)
	if err != nil {
		return ResourceRule{}, "", err
	}
	prefixes, err := validatePaths(document.AllowPathPrefixes, field+".allow_path_prefixes", false)
	if err != nil {
		return ResourceRule{}, "", err
	}
	denyPrefixes, err := validatePaths(document.DenyPathPrefixes, field+".deny_path_prefixes", true)
	if err != nil {
		return ResourceRule{}, "", err
	}
	if len(paths) == 0 && len(prefixes) == 0 {
		return ResourceRule{}, "", invalid(field, "must include an exact path or non-root path prefix")
	}
	if document.AllowedTypes == nil || len(*document.AllowedTypes) == 0 {
		return ResourceRule{}, "", invalid(field+".allowed_types", "must contain script or stylesheet")
	}
	seen := make(map[ResourceType]struct{}, len(*document.AllowedTypes))
	allowedTypes := append([]ResourceType(nil), (*document.AllowedTypes)...)
	for _, resourceType := range *document.AllowedTypes {
		if resourceType != ResourceTypeScript && resourceType != ResourceTypeStylesheet {
			return ResourceRule{}, "", invalid(field+".allowed_types", "unsupported type %q", resourceType)
		}
		if _, duplicate := seen[resourceType]; duplicate {
			return ResourceRule{}, "", invalid(field+".allowed_types", "duplicate type %q", resourceType)
		}
		seen[resourceType] = struct{}{}
	}
	sort.Slice(allowedTypes, func(left, right int) bool {
		return allowedTypes[left] < allowedTypes[right]
	})
	allowedTypeNames := make([]string, len(allowedTypes))
	for index, resourceType := range allowedTypes {
		allowedTypeNames[index] = string(resourceType)
	}
	key := strings.Join([]string{
		host,
		strings.Join(paths, "\x00"),
		strings.Join(prefixes, "\x00"),
		strings.Join(denyPrefixes, "\x00"),
		strings.Join(allowedTypeNames, "\x00"),
	}, "\x01")
	return ResourceRule{
		Host:              host,
		AllowPaths:        paths,
		AllowPathPrefixes: prefixes,
		DenyPathPrefixes:  denyPrefixes,
		AllowedTypes:      allowedTypes,
	}, key, nil
}

func validateHostRule(document *hostRuleDocument, field string) (string, error) {
	if document == nil || document.Host == nil || document.Match == nil {
		return "", invalid(field, "host and match are required")
	}
	host := *document.Host
	if *document.Match != "exact" {
		return "", invalid(field+".match", "must equal exact")
	}
	if len(host) > 253 || !hostnamePattern.MatchString(host) || net.ParseIP(host) != nil {
		return "", invalid(field+".host", "must be a lowercase public DNS hostname")
	}
	for _, suffix := range []string{"localhost", "local", "localdomain", "lan", "home", "home.arpa", "internal", "intranet", "onion", "alt", "arpa", "test", "invalid", "example"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return "", invalid(field+".host", "reserved hostname suffix %q is forbidden", suffix)
		}
	}
	return host, nil
}

func validatePaths(document *[]string, field string, allowRoot bool) ([]string, error) {
	if document == nil {
		return nil, invalid(field, "is required and must not be null")
	}
	values := append([]string(nil), (*document)...)
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		itemField := fmt.Sprintf("%s[%d]", field, index)
		if value == "/" && !allowRoot {
			return nil, invalid(itemField, "root is not a valid allow prefix")
		}
		if err := validatePath(value); err != nil {
			return nil, invalid(itemField, "%v", err)
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, invalid(itemField, "duplicate path %q", value)
		}
		seen[value] = struct{}{}
	}
	sort.Strings(values)
	return values, nil
}

func validatePath(path string) error {
	if path == "" || len(path) > 2029 || !strings.HasPrefix(path, "/") {
		return fmt.Errorf("must be an absolute path of at most 2029 bytes")
	}
	if strings.Contains(path, "//") || strings.ContainsAny(path, "?#") {
		return fmt.Errorf("contains an ambiguous separator, query, or fragment")
	}
	parsed, err := url.Parse("https://render-policy.invalid" + path)
	if err != nil || parsed.EscapedPath() != path || !utils.IsUnambiguousFetchPath(parsed) {
		return fmt.Errorf("is not an unambiguous canonical fetch path")
	}
	return nil
}

func validateNetworkControls(document *networkControlsDocument, field string) error {
	if document == nil || document.AllowedMethods == nil || len(*document.AllowedMethods) != 1 || (*document.AllowedMethods)[0] != "GET" {
		return invalid(field+".allowed_methods", "must equal [GET]")
	}
	if document.RobotsForResources == nil || !*document.RobotsForResources {
		return invalid(field+".robots_for_resources", "must equal true")
	}
	controls := []struct {
		name  string
		value *bool
	}{
		{"allow_cookies", document.AllowCookies},
		{"allow_service_workers", document.AllowServiceWorkers},
		{"allow_websockets", document.AllowWebSockets},
		{"allow_webrtc", document.AllowWebRTC},
		{"allow_downloads", document.AllowDownloads},
		{"allow_popups", document.AllowPopups},
		{"allow_secondary_documents", document.AllowSecondaryDocuments},
		{"allow_javascript_navigation", document.AllowJavaScriptNavigation},
	}
	for _, control := range controls {
		if control.value == nil || *control.value {
			return invalid(field+"."+control.name, "must equal false")
		}
	}
	return nil
}

func validateLimits(document *limitsDocument, field string) (Limits, error) {
	if document == nil || document.MaxRenderTimeMS == nil || document.SettleTimeMS == nil ||
		document.MaxResourceRequests == nil || document.MaxAggregateResourceBytes == nil ||
		document.MaxResourceBodyBytes == nil || document.MaxRenderedDOMBytes == nil ||
		document.MaxDOMNodes == nil || document.MaxRedirectHops == nil || document.MaxConsoleBytes == nil {
		return Limits{}, invalid(field, "all limit fields are required and must not be null")
	}
	if *document.MaxRenderTimeMS < 1 || *document.MaxRenderTimeMS > 30000 {
		return Limits{}, invalid(field+".max_render_time_ms", "must be between 1 and 30000")
	}
	if *document.SettleTimeMS < 0 || *document.SettleTimeMS > 5000 {
		return Limits{}, invalid(field+".settle_time_ms", "must be between 0 and 5000")
	}
	if *document.MaxResourceRequests < 0 || *document.MaxResourceRequests > 64 {
		return Limits{}, invalid(field+".max_resource_requests", "must be between 0 and 64")
	}
	if *document.MaxAggregateResourceBytes < 0 || *document.MaxAggregateResourceBytes > 32*1024*1024 {
		return Limits{}, invalid(field+".max_aggregate_resource_bytes", "must be between 0 and 33554432")
	}
	if *document.MaxResourceBodyBytes < 0 || *document.MaxResourceBodyBytes > 5*1024*1024 {
		return Limits{}, invalid(field+".max_resource_body_bytes", "must be between 0 and 5242880")
	}
	if *document.MaxRenderedDOMBytes < 1 || *document.MaxRenderedDOMBytes > 5*1024*1024 {
		return Limits{}, invalid(field+".max_rendered_dom_bytes", "must be between 1 and 5242880")
	}
	if *document.MaxDOMNodes < 1 || *document.MaxDOMNodes > 100000 {
		return Limits{}, invalid(field+".max_dom_nodes", "must be between 1 and 100000")
	}
	if *document.MaxRedirectHops < 0 || *document.MaxRedirectHops > 3 {
		return Limits{}, invalid(field+".max_redirect_hops", "must be between 0 and 3")
	}
	if *document.MaxConsoleBytes < 0 || *document.MaxConsoleBytes > 64*1024 {
		return Limits{}, invalid(field+".max_console_bytes", "must be between 0 and 65536")
	}
	return Limits{
		MaxRenderTime:             durationMilliseconds(*document.MaxRenderTimeMS),
		SettleTime:                durationMilliseconds(*document.SettleTimeMS),
		MaxResourceRequests:       *document.MaxResourceRequests,
		MaxAggregateResourceBytes: *document.MaxAggregateResourceBytes,
		MaxResourceBodyBytes:      *document.MaxResourceBodyBytes,
		MaxRenderedDOMBytes:       *document.MaxRenderedDOMBytes,
		MaxDOMNodes:               *document.MaxDOMNodes,
		MaxRedirectHops:           *document.MaxRedirectHops,
		MaxConsoleBytes:           *document.MaxConsoleBytes,
	}, nil
}

func durationMilliseconds(value int) time.Duration {
	return time.Duration(value) * time.Millisecond
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func invalid(field, format string, arguments ...any) error {
	return fmt.Errorf("invalid render policy field %s: %s", field, fmt.Sprintf(format, arguments...))
}
