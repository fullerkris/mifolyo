package renderpolicy

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func brokeredPolicyDocument(enabled bool) string {
	scriptRule := `{
		"host_rule": {"host": "cdn.example.org", "match": "exact"},
		"allow_paths": ["/assets/app.js", "/assets/private/exact.js"],
		"allow_path_prefixes": ["/static/"],
		"deny_path_prefixes": ["/assets/private/", "/static/private/"],
		"allowed_types": ["script"]
	}`
	stylesheetRule := `{
		"host_rule": {"host": "cdn.example.org", "match": "exact"},
		"allow_paths": ["/assets/app.css"],
		"allow_path_prefixes": ["/styles/"],
		"deny_path_prefixes": ["/styles/private/"],
		"allowed_types": ["stylesheet"]
	}`
	document := inlinePolicyDocument(enabled)
	document = strings.Replace(document, `"mode": "inline_only"`, `"mode": "brokered"`, 1)
	document = strings.Replace(document, `"resource_rules": []`, `"resource_rules": [`+scriptRule+`,`+stylesheetRule+`]`, 1)
	document = strings.Replace(document, `"max_resource_requests": 0`, `"max_resource_requests": 4`, 1)
	document = strings.Replace(document, `"max_aggregate_resource_bytes": 0`, `"max_aggregate_resource_bytes": 4096`, 1)
	document = strings.Replace(document, `"max_resource_body_bytes": 0`, `"max_resource_body_bytes": 2048`, 1)
	return document
}

func TestRuleMatchResourceAllowsExactPathsAndPrefixes(t *testing.T) {
	policy, err := Decode(strings.NewReader(brokeredPolicyDocument(true)))
	if err != nil {
		t.Fatal(err)
	}
	rule, err := policy.Match("https://render.example.org/app")
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name         string
		rawURL       string
		resourceType ResourceType
	}{
		{
			name:         "exact script",
			rawURL:       "https://cdn.example.org/assets/app.js",
			resourceType: ResourceTypeScript,
		},
		{
			name:         "exact stylesheet",
			rawURL:       "https://cdn.example.org/assets/app.css",
			resourceType: ResourceTypeStylesheet,
		},
		{
			name:         "script prefix",
			rawURL:       "https://cdn.example.org/static/chunks/app.js",
			resourceType: ResourceTypeScript,
		},
		{
			name:         "stylesheet prefix",
			rawURL:       "https://cdn.example.org/styles/site.css",
			resourceType: ResourceTypeStylesheet,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			canonical, matchErr := rule.MatchResource(test.rawURL, test.resourceType)
			if matchErr != nil {
				t.Fatalf("MatchResource(%q, %q) error = %v", test.rawURL, test.resourceType, matchErr)
			}
			if canonical != test.rawURL {
				t.Fatalf("canonical URL = %q, want %q", canonical, test.rawURL)
			}
		})
	}
}

func TestRuleMatchResourceRequiresExactHostPathAndType(t *testing.T) {
	policy, err := Decode(strings.NewReader(brokeredPolicyDocument(true)))
	if err != nil {
		t.Fatal(err)
	}
	rule, err := policy.Match("https://render.example.org/app")
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name         string
		rawURL       string
		resourceType ResourceType
	}{
		{
			name:         "different host",
			rawURL:       "https://assets.example.org/assets/app.js",
			resourceType: ResourceTypeScript,
		},
		{
			name:         "subdomain",
			rawURL:       "https://sub.cdn.example.org/assets/app.js",
			resourceType: ResourceTypeScript,
		},
		{
			name:         "exact path does not match a child",
			rawURL:       "https://cdn.example.org/assets/app.js/child",
			resourceType: ResourceTypeScript,
		},
		{
			name:         "prefix requires its literal slash",
			rawURL:       "https://cdn.example.org/static",
			resourceType: ResourceTypeScript,
		},
		{
			name:         "wrong type",
			rawURL:       "https://cdn.example.org/assets/app.js",
			resourceType: ResourceTypeStylesheet,
		},
		{
			name:         "unsupported type",
			rawURL:       "https://cdn.example.org/assets/app.js",
			resourceType: ResourceType("image"),
		},
		{
			name:         "exact allow loses to deny prefix",
			rawURL:       "https://cdn.example.org/assets/private/exact.js",
			resourceType: ResourceTypeScript,
		},
		{
			name:         "allow prefix loses to deny prefix",
			rawURL:       "https://cdn.example.org/static/private/secret.js",
			resourceType: ResourceTypeScript,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			canonical, matchErr := rule.MatchResource(test.rawURL, test.resourceType)
			if canonical != "" {
				t.Fatalf("denied canonical URL = %q", canonical)
			}
			if !errors.Is(matchErr, ErrNoMatchingResourceRule) {
				t.Fatalf("MatchResource(%q, %q) error = %v", test.rawURL, test.resourceType, matchErr)
			}
		})
	}
}

func TestRuleMatchResourceRejectsUnsafeOrNonCanonicalURLs(t *testing.T) {
	policy, err := Decode(strings.NewReader(brokeredPolicyDocument(true)))
	if err != nil {
		t.Fatal(err)
	}
	rule, err := policy.Match("https://render.example.org/app")
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name   string
		rawURL string
	}{
		{name: "non-canonical host case", rawURL: "https://CDN.example.org/assets/app.js"},
		{name: "query", rawURL: "https://cdn.example.org/assets/app.js?v=1"},
		{name: "empty query", rawURL: "https://cdn.example.org/assets/app.js?"},
		{name: "fragment", rawURL: "https://cdn.example.org/assets/app.js#entry"},
		{name: "userinfo", rawURL: "https://user@cdn.example.org/assets/app.js"},
		{name: "HTTP", rawURL: "http://cdn.example.org/assets/app.js"},
		{name: "non-default port", rawURL: "https://cdn.example.org:8443/assets/app.js"},
		{name: "IP literal is not crawl eligible", rawURL: "https://127.0.0.1/assets/app.js"},
		{name: "ambiguous path", rawURL: "https://cdn.example.org/static//app.js"},
	} {
		t.Run(test.name, func(t *testing.T) {
			canonical, matchErr := rule.MatchResource(test.rawURL, ResourceTypeScript)
			if canonical != "" {
				t.Fatalf("denied canonical URL = %q", canonical)
			}
			if matchErr == nil {
				t.Fatalf("MatchResource(%q) unexpectedly succeeded", test.rawURL)
			}
		})
	}
}

func TestPolicyResourceRulesAreRetainedAndDeepCloned(t *testing.T) {
	policy, err := Decode(strings.NewReader(brokeredPolicyDocument(true)))
	if err != nil {
		t.Fatal(err)
	}

	rules := policy.Rules()
	if len(rules) != 1 || len(rules[0].ResourceRules) != 2 {
		t.Fatalf("decoded rules = %#v", rules)
	}
	scriptRule := rules[0].ResourceRules[0]
	if scriptRule.Host != "cdn.example.org" {
		t.Fatalf("resource host = %q", scriptRule.Host)
	}
	if !reflect.DeepEqual(scriptRule.AllowPaths, []string{"/assets/app.js", "/assets/private/exact.js"}) {
		t.Fatalf("resource allow paths = %#v", scriptRule.AllowPaths)
	}
	if !reflect.DeepEqual(scriptRule.AllowPathPrefixes, []string{"/static/"}) {
		t.Fatalf("resource allow prefixes = %#v", scriptRule.AllowPathPrefixes)
	}
	if !reflect.DeepEqual(scriptRule.DenyPathPrefixes, []string{"/assets/private/", "/static/private/"}) {
		t.Fatalf("resource deny prefixes = %#v", scriptRule.DenyPathPrefixes)
	}
	if !reflect.DeepEqual(scriptRule.AllowedTypes, []ResourceType{ResourceTypeScript}) {
		t.Fatalf("resource allowed types = %#v", scriptRule.AllowedTypes)
	}

	rules[0].ResourceRules[0].Host = "mutated.example.org"
	rules[0].ResourceRules[0].AllowPaths[0] = "/mutated.js"
	rules[0].ResourceRules[0].AllowPathPrefixes[0] = "/mutated/"
	rules[0].ResourceRules[0].DenyPathPrefixes[0] = "/mutated-private/"
	rules[0].ResourceRules[0].AllowedTypes[0] = ResourceTypeStylesheet
	rules[0].ResourceRules[1] = ResourceRule{}
	rules[0].ResourceRules = append(rules[0].ResourceRules, ResourceRule{Host: "extra.example.org"})

	fresh := policy.Rules()[0]
	if len(fresh.ResourceRules) != 2 || fresh.ResourceRules[0].Host != "cdn.example.org" {
		t.Fatalf("mutating Rules changed policy resource rules: %#v", fresh.ResourceRules)
	}
	if fresh.ResourceRules[0].AllowPaths[0] != "/assets/app.js" ||
		fresh.ResourceRules[0].AllowPathPrefixes[0] != "/static/" ||
		fresh.ResourceRules[0].DenyPathPrefixes[0] != "/assets/private/" ||
		fresh.ResourceRules[0].AllowedTypes[0] != ResourceTypeScript {
		t.Fatalf("mutating nested slices changed policy resource rule: %#v", fresh.ResourceRules[0])
	}
	if fresh.ResourceRules[1].Host != "cdn.example.org" {
		t.Fatalf("mutating a resource rule changed its policy copy: %#v", fresh.ResourceRules[1])
	}

	matched, err := policy.Match("https://render.example.org/app")
	if err != nil {
		t.Fatal(err)
	}
	matched.ResourceRules[0].AllowPaths[0] = "/changed-again.js"
	matched.ResourceRules[0].AllowedTypes[0] = ResourceTypeStylesheet
	rematched, err := policy.Match("https://render.example.org/app")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rematched.MatchResource("https://cdn.example.org/assets/app.js", ResourceTypeScript); err != nil {
		t.Fatalf("mutating a matched Rule changed policy matching: %v", err)
	}
}

func TestRuleMatchResourceRejectsDisabledAndInlineRules(t *testing.T) {
	policy, err := Decode(strings.NewReader(brokeredPolicyDocument(true)))
	if err != nil {
		t.Fatal(err)
	}
	rule := policy.Rules()[0]

	disabled := cloneRule(rule)
	disabled.Enabled = false
	if _, err := disabled.MatchResource("https://cdn.example.org/assets/app.js", ResourceTypeScript); !errors.Is(err, ErrNoMatchingResourceRule) {
		t.Fatalf("disabled MatchResource error = %v", err)
	}

	inline := cloneRule(rule)
	inline.Mode = ModeInlineOnly
	if _, err := inline.MatchResource("https://cdn.example.org/assets/app.js", ResourceTypeScript); !errors.Is(err, ErrNoMatchingResourceRule) {
		t.Fatalf("inline MatchResource error = %v", err)
	}
}
