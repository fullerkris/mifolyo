package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type canonicalizationFixtures struct {
	Version     int                 `json:"version"`
	IDNamespace string              `json:"id_namespace"`
	MaxURLBytes int                 `json:"max_url_bytes"`
	Valid       []validURLFixture   `json:"valid"`
	Invalid     []invalidURLFixture `json:"invalid"`
}

type validURLFixture struct {
	Name           string  `json:"name"`
	Input          string  `json:"input"`
	CanonicalURL   string  `json:"canonical_url"`
	URLID          string  `json:"url_id"`
	CrawlEligible  bool    `json:"crawl_eligible"`
	CrawlRejection *string `json:"crawl_rejection"`
}

type invalidURLFixture struct {
	Name  string `json:"name"`
	Input string `json:"input"`
	Error string `json:"error"`
}

func loadCanonicalizationFixtures(t *testing.T) canonicalizationFixtures {
	t.Helper()

	localFixturePath := filepath.Join("testdata", "url-canonicalization-v1.json")
	localData, err := os.ReadFile(localFixturePath)
	if err != nil {
		t.Fatalf("read service-local canonicalization fixture: %v", err)
	}

	repositoryRoot := filepath.Join("..", "..", "..", "..")
	authoritativePath := filepath.Join(repositoryRoot, "contracts", "url-canonicalization", "v1.json")
	authoritativeData, err := os.ReadFile(authoritativePath)
	data := localData
	switch {
	case err == nil:
		assertEquivalentJSON(t, authoritativePath, authoritativeData, localFixturePath, localData)
		data = authoritativeData
	case !os.IsNotExist(err):
		t.Fatalf("read authoritative canonicalization fixture: %v", err)
	default:
		// A normal checkout has a .git file or directory at repositoryRoot and
		// must never fall back when its authoritative fixture is missing. The
		// service-only Docker context intentionally has neither repositoryRoot
		// metadata nor root contracts, so it executes the synchronized copy.
		if _, statErr := os.Stat(filepath.Join(repositoryRoot, ".git")); statErr == nil {
			t.Fatalf("authoritative canonicalization fixture is missing: %s", authoritativePath)
		} else if !os.IsNotExist(statErr) {
			t.Fatalf("inspect repository marker: %v", statErr)
		}
	}

	var fixtures canonicalizationFixtures
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatalf("decode shared canonicalization fixture: %v", err)
	}
	return fixtures
}

func assertEquivalentJSON(t *testing.T, authoritativePath string, authoritativeData []byte, localPath string, localData []byte) {
	t.Helper()

	var authoritativeDocument any
	if err := json.Unmarshal(authoritativeData, &authoritativeDocument); err != nil {
		t.Fatalf("decode authoritative fixture %s: %v", authoritativePath, err)
	}
	var localDocument any
	if err := json.Unmarshal(localData, &localDocument); err != nil {
		t.Fatalf("decode service-local fixture %s: %v", localPath, err)
	}
	if !reflect.DeepEqual(authoritativeDocument, localDocument) {
		t.Fatalf("service-local fixture %s is not synchronized with %s", localPath, authoritativePath)
	}
}

func TestCanonicalizeURLV1SharedFixtures(t *testing.T) {
	fixtures := loadCanonicalizationFixtures(t)
	if fixtures.Version != URLCanonicalizationVersionV1 {
		t.Fatalf("fixture version %d does not match implementation version %d", fixtures.Version, URLCanonicalizationVersionV1)
	}
	if fixtures.IDNamespace != URLIDNamespaceV1 {
		t.Fatalf("fixture namespace %q does not match implementation namespace %q", fixtures.IDNamespace, URLIDNamespaceV1)
	}
	if fixtures.MaxURLBytes != MaxURLBytesV1 {
		t.Fatalf("fixture max URL bytes %d does not match implementation max %d", fixtures.MaxURLBytes, MaxURLBytesV1)
	}

	for _, fixture := range fixtures.Valid {
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			result, err := CanonicalizeURLV1(fixture.Input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.CanonicalURL != fixture.CanonicalURL {
				t.Errorf("canonical URL: got %q, want %q", result.CanonicalURL, fixture.CanonicalURL)
			}
			if result.URLID != fixture.URLID {
				t.Errorf("URL ID: got %q, want %q", result.URLID, fixture.URLID)
			}
			if result.CrawlEligible != fixture.CrawlEligible {
				t.Errorf("crawl eligibility: got %v, want %v", result.CrawlEligible, fixture.CrawlEligible)
			}
			expectedRejection := ""
			if fixture.CrawlRejection != nil {
				expectedRejection = *fixture.CrawlRejection
			}
			if result.CrawlRejection != expectedRejection {
				t.Errorf("crawl rejection: got %q, want %q", result.CrawlRejection, expectedRejection)
			}
			if result.CanonicalizationVersion != fixtures.Version {
				t.Errorf("canonicalization version: got %d, want %d", result.CanonicalizationVersion, fixtures.Version)
			}

			idempotent, err := CanonicalizeURLV1(result.CanonicalURL)
			if err != nil {
				t.Fatalf("canonical URL was not accepted: %v", err)
			}
			if idempotent != result {
				t.Errorf("canonicalization is not idempotent: got %#v, want %#v", idempotent, result)
			}
		})
	}

	for _, fixture := range fixtures.Invalid {
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			_, err := CanonicalizeURLV1(fixture.Input)
			if err == nil {
				t.Fatal("expected an error")
			}
			if code := CanonicalizationErrorCode(err); code != fixture.Error {
				t.Errorf("error code: got %q (%v), want %q", code, err, fixture.Error)
			}
		})
	}
}

func TestCanonicalizeURLV1ComponentEncodingAndPortNormalization(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "unsafe ASCII component bytes",
			input:    "https://example.com/a path/{item}?value=a b|c",
			expected: "https://example.com/a%20path/%7Bitem%7D?value=a%20b%7Cc",
		},
		{
			name:     "non-default port has one numeric form",
			input:    "https://example.com:08443/path",
			expected: "https://example.com:8443/path",
		},
		{
			name:     "zero-padded default port is removed",
			input:    "http://example.com:00080/path",
			expected: "http://example.com/path",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := CanonicalizeURLV1(test.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.CanonicalURL != test.expected {
				t.Errorf("got %q, want %q", result.CanonicalURL, test.expected)
			}
		})
	}
}

func TestCanonicalizeURLV1AdditionalStableErrors(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		errorCode string
	}{
		{name: "dotted numeric lookalike", input: "https://999.999.999.999/path", errorCode: ErrInvalidHost.Code},
		{name: "percent-encoded host", input: "https://%65xample.com/path", errorCode: ErrInvalidHost.Code},
		{name: "empty-host userinfo", input: "https://user@/path", errorCode: ErrUserinfoForbidden.Code},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := CanonicalizeURLV1(test.input)
			if code := CanonicalizationErrorCode(err); code != test.errorCode {
				t.Fatalf("error code = %q, want %q (error: %v)", code, test.errorCode, err)
			}
		})
	}
}

func TestCanonicalizeURLV1RejectsEncodedControls(t *testing.T) {
	inputs := []string{
		"https://example.com/%00",
		"https://example.com/%1f",
		"https://example.com/%7F",
		"https://example.com/%C2%85",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			_, err := CanonicalizeURLV1(input)
			if code := CanonicalizationErrorCode(err); code != ErrEncodedControl.Code {
				t.Fatalf("error code = %q, want %q (error: %v)", code, ErrEncodedControl.Code, err)
			}
		})
	}
}

func TestCanonicalizeURLV1StaticCrawlAdmission(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		rejection string
	}{
		{name: "IPv6 literal", input: "https://[::1]/", rejection: CrawlRejectionIPLiteral},
		{name: "single label", input: "https://printer/path", rejection: CrawlRejectionLocalName},
		{name: "local suffix", input: "https://service.local/path", rejection: CrawlRejectionLocalName},
		{name: "onion suffix", input: "https://service.onion/path", rejection: CrawlRejectionLocalName},
		{name: "alternative namespace", input: "https://service.alt/path", rejection: CrawlRejectionLocalName},
		{name: "infrastructure namespace", input: "https://service.arpa/path", rejection: CrawlRejectionLocalName},
		{name: "non-default port", input: "https://example.com:444/path", rejection: CrawlRejectionNonDefaultPort},
		{name: "default port", input: "https://example.com:443/path", rejection: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := CanonicalizeURLV1(test.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.CrawlRejection != test.rejection {
				t.Errorf("got rejection %q, want %q", result.CrawlRejection, test.rejection)
			}
			if result.CrawlEligible != (test.rejection == "") {
				t.Errorf("eligibility %v does not match rejection %q", result.CrawlEligible, test.rejection)
			}
		})
	}
}

func TestURLIDV1Shape(t *testing.T) {
	result, err := CanonicalizeURLV1("https://example.com/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !IsURLIDV1(result.URLID) {
		t.Fatalf("expected a valid V1 URL ID, got %q", result.URLID)
	}
	if IsURLIDV1("HTTPS://example.com/") {
		t.Fatal("an absolute URL must not be accepted as an opaque URL ID")
	}
}
