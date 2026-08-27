package renderpolicy

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func inlinePolicyDocument(enabled bool) string {
	return `{
		"schema_version": 1,
		"default_action": "deny",
		"rules": [{
			"id": "inline-fixture",
			"enabled": ` + map[bool]string{true: "true", false: "false"}[enabled] + `,
			"host_rule": {"host": "render.example.org", "match": "exact"},
			"allow_paths": ["/app"],
			"allow_path_prefixes": ["/docs/"],
			"deny_path_prefixes": ["/docs/private/"],
			"mode": "inline_only",
			"failure_action": "reject_page",
			"resource_rules": [],
			"network_controls": {
				"allowed_methods": ["GET"],
				"robots_for_resources": true,
				"allow_cookies": false,
				"allow_service_workers": false,
				"allow_websockets": false,
				"allow_webrtc": false,
				"allow_downloads": false,
				"allow_popups": false,
				"allow_secondary_documents": false,
				"allow_javascript_navigation": false
			},
			"limits": {
				"max_render_time_ms": 5000,
				"settle_time_ms": 50,
				"max_resource_requests": 0,
				"max_aggregate_resource_bytes": 0,
				"max_resource_body_bytes": 0,
				"max_rendered_dom_bytes": 1048576,
				"max_dom_nodes": 1000,
				"max_redirect_hops": 0,
				"max_console_bytes": 1024
			}
		}]
	}`
}

func TestInlinePolicyMatchesOnlyReviewedExactHostAndPaths(t *testing.T) {
	policy, err := Decode(strings.NewReader(inlinePolicyDocument(true)))
	if err != nil {
		t.Fatal(err)
	}
	if policy.EnabledRuleCount() != 1 {
		t.Fatalf("enabled rules = %d", policy.EnabledRuleCount())
	}
	for _, rawURL := range []string{
		"https://render.example.org/app",
		"https://render.example.org/docs/guide?lang=en",
	} {
		rule, matchErr := policy.Match(rawURL)
		if matchErr != nil || rule.ID != "inline-fixture" || rule.Limits.MaxRenderTime != 5*time.Second {
			t.Fatalf("Match(%q) = %#v, %v", rawURL, rule, matchErr)
		}
	}
	for _, rawURL := range []string{
		"http://render.example.org/app",
		"https://sub.render.example.org/app",
		"https://render.example.org/other",
		"https://render.example.org/docs/private/secret",
	} {
		if _, matchErr := policy.Match(rawURL); !errors.Is(matchErr, ErrNoMatchingRule) {
			t.Fatalf("Match(%q) error = %v", rawURL, matchErr)
		}
	}
}

func TestDisabledPolicyNeverMatches(t *testing.T) {
	policy, err := Decode(strings.NewReader(inlinePolicyDocument(false)))
	if err != nil {
		t.Fatal(err)
	}
	if policy.EnabledRuleCount() != 0 {
		t.Fatalf("enabled rules = %d", policy.EnabledRuleCount())
	}
	if _, err := policy.Match("https://render.example.org/app"); !errors.Is(err, ErrNoMatchingRule) {
		t.Fatalf("disabled rule match error = %v", err)
	}
}

func TestPolicyDecoderRejectsAmbiguousDocumentsAndUnsafeControls(t *testing.T) {
	valid := inlinePolicyDocument(true)
	for name, document := range map[string]string{
		"duplicate":   strings.Replace(valid, `"schema_version": 1`, `"schema_version": 1, "schema_version": 1`, 1),
		"unknown":     strings.Replace(valid, `"default_action": "deny"`, `"default_action": "deny", "extra": true`, 1),
		"cookies":     strings.Replace(valid, `"allow_cookies": false`, `"allow_cookies": true`, 1),
		"root prefix": strings.Replace(valid, `"allow_path_prefixes": ["/docs/"]`, `"allow_path_prefixes": ["/"]`, 1),
		"resources":   strings.Replace(valid, `"resource_rules": []`, `"resource_rules": [{"host_rule":{"host":"cdn.example.org","match":"exact"},"allow_paths":["/app.js"],"allow_path_prefixes":[],"deny_path_prefixes":[],"allowed_types":["script"]}]`, 1),
		"redirects":   strings.Replace(valid, `"max_redirect_hops": 0`, `"max_redirect_hops": 1`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(strings.NewReader(document)); err == nil {
				t.Fatal("invalid render policy was accepted")
			}
		})
	}
}

func TestPolicyDecoderRejectsDuplicateBrokeredResourceRules(t *testing.T) {
	resource := `{"host_rule":{"host":"cdn.example.org","match":"exact"},"allow_paths":["/app.js"],"allow_path_prefixes":[],"deny_path_prefixes":[],"allowed_types":["script"]}`
	document := strings.Replace(inlinePolicyDocument(false), `"mode": "inline_only"`, `"mode": "brokered"`, 1)
	document = strings.Replace(document, `"resource_rules": []`, `"resource_rules": [`+resource+`]`, 1)
	document = strings.Replace(document, `"max_resource_requests": 0`, `"max_resource_requests": 1`, 1)
	document = strings.Replace(document, `"max_aggregate_resource_bytes": 0`, `"max_aggregate_resource_bytes": 1024`, 1)
	document = strings.Replace(document, `"max_resource_body_bytes": 0`, `"max_resource_body_bytes": 1024`, 1)
	if _, err := Decode(strings.NewReader(document)); err != nil {
		t.Fatalf("single brokered resource rule was rejected: %v", err)
	}
	redirecting := strings.Replace(document, `"max_redirect_hops": 0`, `"max_redirect_hops": 1`, 1)
	if _, err := Decode(strings.NewReader(redirecting)); err == nil || !strings.Contains(err.Error(), "redirects are not supported") {
		t.Fatalf("brokered redirect error = %v", err)
	}

	duplicate := strings.Replace(document, `"resource_rules": [`+resource+`]`, `"resource_rules": [`+resource+`,`+resource+`]`, 1)
	if _, err := Decode(strings.NewReader(duplicate)); err == nil || !strings.Contains(err.Error(), "duplicates another resource rule") {
		t.Fatalf("duplicate resource rule error = %v", err)
	}
}

func TestTrackedDisabledPolicyLoads(t *testing.T) {
	path := filepath.Join("..", "..", "config", "render-policy-v1.disabled.json")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		t.Skip("config is outside a service-only build context")
	}
	policy, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if policy.EnabledRuleCount() != 0 || len(policy.Rules()) != 0 {
		t.Fatalf("disabled policy = %#v", policy.Rules())
	}
}
