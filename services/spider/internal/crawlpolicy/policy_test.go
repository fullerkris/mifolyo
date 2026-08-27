package crawlpolicy

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

func TestDecodeStrictV1AndFallbackUserAgent(t *testing.T) {
	policy := decodeTestPolicy(t, policyJSON(testGroupJSON("base", "example.com", MatchApexAndSubdomains)), "GlobalBot/1.0")
	if policy.SchemaVersion() != 1 || policy.UnmatchedAction() != UnmatchedDeny {
		t.Fatalf("unexpected policy metadata: version=%d action=%q", policy.SchemaVersion(), policy.UnmatchedAction())
	}
	group, ok := policy.Group("base")
	if !ok {
		t.Fatal("expected base group")
	}
	if group.UserAgent != "GlobalBot/1.0" {
		t.Fatalf("fallback user agent = %q", group.UserAgent)
	}
	if group.MinRequestInterval != 0 || group.Robots.CacheTTL != time.Hour {
		t.Fatalf("durations were not parsed: interval=%s robots=%s", group.MinRequestInterval, group.Robots.CacheTTL)
	}

	customDocument := strings.Replace(policyJSON(testGroupJSON("base", "example.com", MatchExact)), `"redirects":`, `"user_agent":"GroupBot/2.0","redirects":`, 1)
	custom := decodeTestPolicy(t, customDocument, "GlobalBot/1.0")
	customGroup, _ := custom.Group("base")
	if customGroup.UserAgent != "GroupBot/2.0" {
		t.Fatalf("custom user agent = %q", customGroup.UserAgent)
	}

	// Accessors must not expose slices used internally by matching.
	customGroup.HostRules[0].Host = "attacker.example"
	again, _ := custom.Group("base")
	if again.HostRules[0].Host != "example.com" {
		t.Fatal("Group returned mutable policy storage")
	}
}

func TestDecodeRejectsMalformedAndUnknownJSON(t *testing.T) {
	valid := policyJSON(testGroupJSON("base", "example.com", MatchExact))
	tests := []struct {
		name     string
		document string
		fallback string
		contains string
	}{
		{name: "wrong version", document: strings.Replace(valid, `"schema_version":1`, `"schema_version":2`, 1), fallback: "Bot", contains: "schema_version"},
		{name: "missing unmatched action", document: strings.Replace(valid, `"unmatched_action":"deny",`, "", 1), fallback: "Bot", contains: "unmatched_action"},
		{name: "unsupported unmatched action", document: strings.Replace(valid, `"unmatched_action":"deny"`, `"unmatched_action":"allow"`, 1), fallback: "Bot", contains: "supports only"},
		{name: "top-level unknown field", document: strings.Replace(valid, `"groups":`, `"mystery":true,"groups":`, 1), fallback: "Bot", contains: "unknown field"},
		{name: "nested unknown field", document: strings.Replace(valid, `"enabled":true`, `"enabled":true,"mystery":true`, 1), fallback: "Bot", contains: "unknown field"},
		{name: "trailing object", document: valid + `{}`, fallback: "Bot", contains: "trailing JSON"},
		{name: "trailing garbage", document: valid + ` nope`, fallback: "Bot", contains: "trailing JSON"},
		{name: "duplicate JSON member", document: strings.Replace(valid, `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1), fallback: "Bot", contains: "duplicate object member"},
		{name: "null optional user agent", document: strings.Replace(valid, `"redirects":`, `"user_agent":null,"redirects":`, 1), fallback: "Bot", contains: "not null"},
		{name: "group user-agent control", document: strings.Replace(valid, `"redirects":`, `"user_agent":"Bad\nAgent","redirects":`, 1), fallback: "Bot", contains: "control"},
		{name: "fallback user-agent control", document: valid, fallback: "Bad\rBot", contains: "control"},
		{name: "empty group user-agent", document: strings.Replace(valid, `"redirects":`, `"user_agent":"","redirects":`, 1), fallback: "Bot", contains: "empty"},
		{name: "untrimmed group user-agent", document: strings.Replace(valid, `"redirects":`, `"user_agent":" Bot ","redirects":`, 1), fallback: "Bot", contains: "leading or trailing"},
		{name: "empty fallback user-agent", document: valid, fallback: "", contains: "empty"},
		{name: "untrimmed fallback user-agent", document: valid, fallback: " Bot", contains: "leading or trailing"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode(strings.NewReader(test.document), test.fallback)
			if err == nil {
				t.Fatal("expected decode error")
			}
			if !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("error %q does not contain %q", err, test.contains)
			}
		})
	}
}

func TestDecodeValidationFailures(t *testing.T) {
	validGroup := testGroupJSON("base", "example.com", MatchExact)
	valid := policyJSON(validGroup)
	duplicateID := policyJSON(validGroup + "," + strings.Replace(validGroup, `"host":"example.com"`, `"host":"other.example.com"`, 1))
	duplicateRule := strings.Replace(valid, `{"host":"example.com","match":"exact"}`, `{"host":"example.com","match":"exact"},{"host":"example.com","match":"exact"}`, 1)
	ambiguous := policyJSON(
		testGroupJSON("exact", "example.com", MatchExact) + "," +
			testGroupJSON("apex", "example.com", MatchApexAndSubdomains),
	)

	tests := []struct {
		name     string
		document string
		contains string
	}{
		{name: "duplicate IDs", document: duplicateID, contains: "duplicate group ID"},
		{name: "duplicate host rules", document: duplicateRule, contains: "duplicate host rule"},
		{name: "equal-specificity overlap", document: ambiguous, contains: "equal-specificity"},
		{name: "uppercase host", document: strings.Replace(valid, `"host":"example.com"`, `"host":"Example.com"`, 1), contains: "lowercase"},
		{name: "wildcard host", document: strings.Replace(valid, `"host":"example.com"`, `"host":"*.example.com"`, 1), contains: "wildcard"},
		{name: "host with scheme", document: strings.Replace(valid, `"host":"example.com"`, `"host":"https://example.com"`, 1), contains: "scheme"},
		{name: "IP host", document: strings.Replace(valid, `"host":"example.com"`, `"host":"127.0.0.1"`, 1), contains: "static crawl safety"},
		{name: "local host", document: strings.Replace(valid, `"host":"example.com"`, `"host":"service.local"`, 1), contains: "static crawl safety"},
		{name: "duplicate scheme", document: strings.Replace(valid, `["https"]`, `["https","https"]`, 1), contains: "duplicate scheme"},
		{name: "unsupported scheme", document: strings.Replace(valid, `["https"]`, `["ftp"]`, 1), contains: "either"},
		{name: "negative depth", document: strings.Replace(valid, `"max_depth":2`, `"max_depth":-1`, 1), contains: "max_depth"},
		{name: "inexact Lua depth", document: strings.Replace(valid, `"max_depth":2`, `"max_depth":9007199254740992`, 1), contains: "max_depth"},
		{name: "zero concurrency", document: strings.Replace(valid, `"max_concurrency":2`, `"max_concurrency":0`, 1), contains: "max_concurrency"},
		{name: "zero batch cap", document: strings.Replace(valid, `"max_requests_per_batch":5`, `"max_requests_per_batch":0`, 1), contains: "max_requests_per_batch"},
		{name: "negative interval", document: strings.Replace(valid, `"min_request_interval":"0s"`, `"min_request_interval":"-1s"`, 1), contains: "non-negative"},
		{name: "invalid interval", document: strings.Replace(valid, `"min_request_interval":"0s"`, `"min_request_interval":"soon"`, 1), contains: "Go duration"},
		{name: "path is relative", document: strings.Replace(valid, `"allow_path_prefixes":[]`, `"allow_path_prefixes":["docs/"]`, 1), contains: "absolute path"},
		{name: "path has query", document: strings.Replace(valid, `"allow_path_prefixes":[]`, `"allow_path_prefixes":["/docs?x=1"]`, 1), contains: "query"},
		{name: "path is not canonical", document: strings.Replace(valid, `"allow_path_prefixes":[]`, `"allow_path_prefixes":["/a b"]`, 1), contains: "canonical"},
		{name: "path has encoded percent", document: strings.Replace(valid, `"allow_path_prefixes":[]`, `"allow_path_prefixes":["/docs/%25value"]`, 1), contains: "unambiguous"},
		{name: "path has double encoded traversal", document: strings.Replace(valid, `"allow_path_prefixes":[]`, `"allow_path_prefixes":["/docs/%252E%252E/private"]`, 1), contains: "unambiguous"},
		{name: "path has invalid UTF-8 escape", document: strings.Replace(valid, `"allow_path_prefixes":[]`, `"allow_path_prefixes":["/docs/%FF"]`, 1), contains: "unambiguous"},
		{name: "path has repeated slash", document: strings.Replace(valid, `"allow_path_prefixes":[]`, `"allow_path_prefixes":["/docs//private"]`, 1), contains: "unambiguous"},
		{name: "duplicate path", document: strings.Replace(valid, `"allow_path_prefixes":[]`, `"allow_path_prefixes":["/docs/","/docs/"]`, 1), contains: "duplicate path"},
		{name: "redirect hops too high", document: strings.Replace(valid, `"max_hops":3`, `"max_hops":11`, 1), contains: "between 0 and 10"},
		{name: "unsupported redirect mode", document: strings.Replace(valid, `"mode":"same_group"`, `"mode":"anywhere"`, 1), contains: "same_host"},
		{name: "robots must enforce", document: strings.Replace(valid, `"mode":"enforce"`, `"mode":"ignore"`, 1), contains: "supports only"},
		{name: "robots TTL positive", document: strings.Replace(valid, `"cache_ttl":"1h"`, `"cache_ttl":"0s"`, 1), contains: "positive"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode(strings.NewReader(test.document), "Bot/1.0")
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("error %q does not contain %q", err, test.contains)
			}
		})
	}
}

func TestMatchingPrecedenceIsIndependentOfPriorityAndFileOrder(t *testing.T) {
	broad := strings.Replace(testGroupJSON("broad", "example.com", MatchApexAndSubdomains), `"priority":10`, `"priority":-100`, 1)
	specific := strings.Replace(testGroupJSON("specific", "docs.example.com", MatchApexAndSubdomains), `"priority":10`, `"priority":100`, 1)
	exact := strings.Replace(testGroupJSON("exact", "api.docs.example.com", MatchExact), `"priority":10`, `"priority":-1000`, 1)

	for name, groups := range map[string]string{
		"forward": broad + "," + specific + "," + exact,
		"reverse": exact + "," + specific + "," + broad,
	} {
		t.Run(name, func(t *testing.T) {
			policy := decodeTestPolicy(t, policyJSON(groups), "Bot")
			assertMatchedGroup(t, policy, "https://api.docs.example.com/page", 0, "exact", MatchExact)
			assertMatchedGroup(t, policy, "https://guide.docs.example.com/page", 0, "specific", MatchApexAndSubdomains)
			assertMatchedGroup(t, policy, "https://www.example.com/page", 0, "broad", MatchApexAndSubdomains)

			_, err := policy.Match("https://badexample.com/page", 0)
			assertDenial(t, err, ReasonUnknownDomain)
		})
	}
}

func TestDisabledExactRuleIsFailClosedCarveOut(t *testing.T) {
	broad := testGroupJSON("broad", "example.com", MatchApexAndSubdomains)
	disabled := strings.Replace(testGroupJSON("blocked", "blocked.example.com", MatchExact), `"enabled":true`, `"enabled":false`, 1)
	policy := decodeTestPolicy(t, policyJSON(broad+","+disabled), "Bot")

	decision, err := policy.Match("https://blocked.example.com/path", 0)
	assertDenial(t, err, ReasonGroupDisabled)
	if decision.Group.ID != "blocked" || decision.MatchedHostRule.Match != MatchExact {
		t.Fatalf("disabled carve-out was not selected: %#v", decision)
	}
	assertMatchedGroup(t, policy, "https://other.example.com/path", 0, "broad", MatchApexAndSubdomains)
}

func TestMatchDenialsAndCanonicalDecision(t *testing.T) {
	group := testGroupJSON("docs", "example.com", MatchApexAndSubdomains)
	group = strings.Replace(group, `"allow_path_prefixes":[]`, `"allow_path_prefixes":["/docs/"]`, 1)
	group = strings.Replace(group, `"deny_path_prefixes":[]`, `"deny_path_prefixes":["/docs/private/"]`, 1)
	group = strings.Replace(group, `"max_depth":2`, `"max_depth":1`, 1)
	policy := decodeTestPolicy(t, policyJSON(group), "Bot")

	tests := []struct {
		name   string
		url    string
		depth  int
		reason DenialReason
	}{
		{name: "unknown domain", url: "https://other.example.org/docs/", depth: 0, reason: ReasonUnknownDomain},
		{name: "scheme", url: "http://example.com/docs/", depth: 0, reason: ReasonSchemeNotAllowed},
		{name: "deny before allow", url: "https://example.com/docs/private/item", depth: 0, reason: ReasonPathDenied},
		{name: "not in allow list", url: "https://example.com/about", depth: 0, reason: ReasonPathNotAllowed},
		{name: "depth exceeded", url: "https://example.com/docs/item", depth: 2, reason: ReasonDepthExceeded},
		{name: "negative depth", url: "https://example.com/docs/item", depth: -1, reason: ReasonInvalidDepth},
		{name: "invalid URL", url: "not a URL", depth: 0, reason: ReasonInvalidURL},
		{name: "encoded unreserved path", url: "https://example.com/docs/%70rivate", depth: 0, reason: ReasonAmbiguousPath},
		{name: "encoded path separator", url: "https://example.com/docs%2Fprivate", depth: 0, reason: ReasonAmbiguousPath},
		{name: "decoded backslash", url: "https://example.com/docs/%5Cprivate", depth: 0, reason: ReasonAmbiguousPath},
		{name: "repeated slash", url: "https://example.com/docs//private", depth: 0, reason: ReasonAmbiguousPath},
		{name: "dot segment", url: "https://example.com/docs/../private", depth: 0, reason: ReasonAmbiguousPath},
		{name: "encoded percent", url: "https://example.com/docs/%25value", depth: 0, reason: ReasonAmbiguousPath},
		{name: "double encoded traversal", url: "https://example.com/docs/%252E%252E/private", depth: 0, reason: ReasonAmbiguousPath},
		{name: "invalid decoded UTF-8", url: "https://example.com/docs/%FF", depth: 0, reason: ReasonAmbiguousPath},
		{name: "overlong UTF-8 slash", url: "https://example.com/docs/%C0%AFprivate", depth: 0, reason: ReasonAmbiguousPath},
		{name: "static IP safety", url: "https://127.0.0.1/docs/", depth: 0, reason: ReasonStaticSafety},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := policy.Match(test.url, test.depth)
			assertDenial(t, err, test.reason)
		})
	}

	decision, err := policy.Match("HTTPS://EXAMPLE.COM/docs/a path?x=1#fragment", 1)
	if err != nil {
		t.Fatalf("valid match failed: %v", err)
	}
	if decision.Identity.CanonicalURL != "https://example.com/docs/a%20path?x=1" {
		t.Fatalf("canonical identity = %q", decision.Identity.CanonicalURL)
	}
	if decision.Scheme != "https" || decision.Host != "example.com" || decision.Path != "/docs/a%20path" || decision.URL == nil {
		t.Fatalf("incomplete decision: %#v", decision)
	}
}

func TestMatchRobotsBypassesOnlyPageScope(t *testing.T) {
	group := testGroupJSON("docs", "example.com", MatchExact)
	group = strings.Replace(group, `"allow_path_prefixes":[]`, `"allow_path_prefixes":["/docs/"]`, 1)
	group = strings.Replace(group, `"max_depth":2`, `"max_depth":0`, 1)
	policy := decodeTestPolicy(t, policyJSON(group), "Bot")

	if _, err := policy.Match("https://example.com/robots.txt", 1); DenialReasonOf(err) != ReasonPathNotAllowed {
		t.Fatalf("page match error = %v, want path denial", err)
	}
	decision, err := policy.MatchRobots("https://example.com/robots.txt")
	if err != nil {
		t.Fatalf("robots match failed: %v", err)
	}
	if decision.Group.ID != "docs" || decision.Path != "/robots.txt" {
		t.Fatalf("unexpected robots decision: %#v", decision)
	}
	if _, err := policy.MatchRobots("https://other.example.org/robots.txt"); DenialReasonOf(err) != ReasonUnknownDomain {
		t.Fatalf("unknown robots host error = %v", err)
	}
	if _, err := policy.MatchRobots("https://example.com/docs/robots-rules"); DenialReasonOf(err) != ReasonPathDenied {
		t.Fatalf("non-standard robots path error = %v, want path denial", err)
	}
	if _, err := policy.MatchRobots("https://example.com/robots.txt?variant=1"); DenialReasonOf(err) != ReasonPathDenied {
		t.Fatalf("robots query error = %v, want path denial", err)
	}
}

func TestSchedulingOrderAndExampleConfig(t *testing.T) {
	groupB := strings.Replace(testGroupJSON("b", "b.example.com", MatchExact), `"priority":10`, `"priority":5`, 1)
	groupA := strings.Replace(testGroupJSON("a", "a.example.com", MatchExact), `"priority":10`, `"priority":5`, 1)
	groupC := strings.Replace(testGroupJSON("c", "c.example.com", MatchExact), `"priority":10`, `"priority":6`, 1)
	policy := decodeTestPolicy(t, policyJSON(groupC+","+groupB+","+groupA), "Bot")
	groups := policy.Groups()
	if got := []string{groups[0].ID, groups[1].ID, groups[2].ID}; strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("scheduling order = %v", got)
	}

	examplePath := filepath.Join("..", "..", "config", "crawl-policy-v1.example.json")
	if _, err := Load(examplePath, "MiFolyoBot/1.0"); err != nil {
		t.Fatalf("example config is invalid: %v", err)
	}
}

func TestBaselinePolicyAdmitsOnlyEnabledDirectManualSeeds(t *testing.T) {
	policy, err := Load(filepath.Join("..", "..", "config", "crawl-policy-v1.baseline.json"), "MiFolyoBot/1.0")
	if err != nil {
		t.Fatalf("load baseline policy: %v", err)
	}
	file, err := os.Open(filepath.Join("..", "..", "..", "..", "seeds", "manual-seeds.csv"))
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("repository seed catalog is outside the service-only Docker build context")
	}
	if err != nil {
		t.Fatalf("open manual seeds: %v", err)
	}
	defer file.Close()
	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatalf("read manual seeds: %v", err)
	}

	disabledURLs := map[string]struct{}{
		"https://www.bbc.com/news":    {},
		"https://www.khanacademy.org": {},
		"https://www.politifact.com":  {},
	}
	admitted := 0
	for index, record := range records[1:] {
		if len(record) < 4 {
			t.Fatalf("seed row %d has %d columns", index+2, len(record))
		}
		if record[3] == "manual_reddit_discovery" {
			continue
		}
		decision, matchErr := policy.Match(record[0], 0)
		if _, disabled := disabledURLs[record[0]]; disabled {
			if decision.Group.ID != "disabled-sites" {
				t.Errorf("disabled seed row %d %s selected group %q", index+2, record[0], decision.Group.ID)
			}
			assertDenial(t, matchErr, ReasonGroupDisabled)
			continue
		}
		if matchErr != nil {
			t.Errorf("seed row %d %s is not admitted: %v", index+2, record[0], matchErr)
		}
		admitted++
	}
	if admitted != 67 {
		t.Fatalf("admitted direct seeds = %d, want 67", admitted)
	}
}

func TestBaselinePolicyKeepsRedditCrawlerDisabled(t *testing.T) {
	policy, err := Load(filepath.Join("..", "..", "config", "crawl-policy-v1.baseline.json"), "MiFolyoBot/1.0")
	if err != nil {
		t.Fatalf("load baseline policy: %v", err)
	}
	group, exists := policy.Group("reddit-crawler")
	if !exists {
		t.Fatal("reddit-crawler group is missing")
	}
	if group.Enabled {
		t.Fatal("reddit-crawler group must remain disabled pending approved access")
	}
	if len(group.HostRules) != 1 || group.HostRules[0] != (HostRule{Host: "reddit.com", Match: MatchApexAndSubdomains}) {
		t.Fatalf("reddit-crawler host rules = %#v", group.HostRules)
	}

	for _, rawURL := range []string{
		"https://www.reddit.com/r/games",
		"https://old.reddit.com/r/games",
		"https://www.reddit.com/r/games.json",
		"https://old.reddit.com/r/games.json",
	} {
		decision, matchErr := policy.Match(rawURL, 0)
		if decision.Group.ID != "reddit-crawler" {
			t.Fatalf("Match(%q) selected group %q", rawURL, decision.Group.ID)
		}
		assertDenial(t, matchErr, ReasonGroupDisabled)
	}
}

func assertMatchedGroup(t *testing.T, policy *Policy, rawURL string, depth int, groupID string, match HostMatch) {
	t.Helper()
	decision, err := policy.Match(rawURL, depth)
	if err != nil {
		t.Fatalf("Match(%q) failed: %v", rawURL, err)
	}
	if decision.Group.ID != groupID || decision.MatchedHostRule.Match != match {
		t.Fatalf("Match(%q) selected %q/%q, want %q/%q", rawURL, decision.Group.ID, decision.MatchedHostRule.Match, groupID, match)
	}
}

func assertDenial(t *testing.T, err error, reason DenialReason) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected denial %q", reason)
	}
	if got := DenialReasonOf(err); got != reason {
		t.Fatalf("denial reason = %q (%v), want %q", got, err, reason)
	}
	var denial *DenialError
	if !errors.As(err, &denial) || denial.Code() != string(reason) {
		t.Fatalf("error is not a typed denial with code %q: %T %v", reason, err, err)
	}
}

func decodeTestPolicy(t *testing.T, document, fallback string) *Policy {
	t.Helper()
	policy, err := Decode(strings.NewReader(document), fallback)
	if err != nil {
		t.Fatalf("Decode failed: %v\n%s", err, document)
	}
	return policy
}

func policyJSON(groups string) string {
	return fmt.Sprintf(`{"schema_version":1,"unmatched_action":"deny","groups":[%s]}`, groups)
}

func testGroupJSON(id, host string, match HostMatch) string {
	return fmt.Sprintf(`{
        "id":%q,
        "enabled":true,
        "priority":10,
        "host_rules":[{"host":%q,"match":%q}],
        "allowed_schemes":["https"],
        "max_depth":2,
        "allow_path_prefixes":[],
        "deny_path_prefixes":[],
        "min_request_interval":"0s",
        "max_concurrency":2,
        "max_requests_per_batch":5,
        "redirects":{"mode":"same_group","max_hops":3},
        "robots":{"mode":"enforce","on_error":"deny","cache_ttl":"1h"}
    }`, id, host, match)
}

func TestStaticDenialPreservesUnderlyingReason(t *testing.T) {
	policy := decodeTestPolicy(t, policyJSON(testGroupJSON("base", "example.com", MatchExact)), "Bot")
	_, err := policy.Match("https://127.0.0.1/", 0)
	var admission *utils.CrawlAdmissionError
	if !errors.As(err, &admission) || admission.Rejection != utils.CrawlRejectionIPLiteral {
		t.Fatalf("static reason was not preserved: %v", err)
	}
}
