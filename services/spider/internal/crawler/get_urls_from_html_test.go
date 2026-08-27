package crawler

// Tests taken from www.boot.dev - Web Crawler

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

func TestGetURLsFromHTML(t *testing.T) {
	tests := []struct {
		name      string
		inputURL  string
		inputBody string
		expected  []string
	}{
		{
			name:     "absolute and relative URLs",
			inputURL: "https://randomsite.com",
			inputBody: `
            <html>
                <body>
                    <h1>Some text here</h1>
                    <a href="/path/one">
                        <span>randomsite</span>
                    </a>
                    <a href="https://othersite.com/path/one">
                        <span>othersite.com</span>
                    </a>
                </body>
            </html>
            `,
			expected: []string{"https://randomsite.com/path/one", "https://othersite.com/path/one"},
		},
		{
			name:     "no URLs",
			inputURL: "https://randomsite.com",
			inputBody: `
            <html>
                <body>
                    <h1>Empty Website</h1>
                </body>
            </html>
            `,
			expected: []string{},
		},
		{
			name:     "malformed HTML but valid links",
			inputURL: "https://example.com",
			inputBody: `
            <html>
                <body>
                    <a href="/valid-link"><span>Valid</span></a>
                    <a href="<invalid></a>"><span>Broken</span></a>
                    <a href="https://valid.com/path"></a>
                </body>
            </html>
            `,
			expected: []string{"https://example.com/valid-link", "https://valid.com/path"},
		},
		{
			name:     "remove duplicate links",
			inputURL: "https://example.com",
			inputBody: `
            <html>
                <body>
                    <a href="/valid-link"><span>Valid</span></a>
                    <a href="<invalid></a>"><span>Broken</span></a>
                    <a href="https://valid.com/path"></a>
                    <a href="/valid-link"><span>Valid</span></a>
                    <a href="<invalid></a>"><span>Broken</span></a>
                    <a href="https://valid.com/path"></a>
                </body>
            </html>
            `,
			expected: []string{"https://example.com/valid-link", "https://valid.com/path"},
		},
		{
			name:     "ignore non-ASCII links",
			inputURL: "https://example.com",
			inputBody: `
            <html>
                <body>
                    <a href="/valid-link"><span>Valid</span></a>
                    <a href="https://valid.com/path"></a>
                    <a href="https://пример.рф">Cyrillic</a>
                    <a href="https://例子.com">Chinese</a>
                    <a href="https://テスト.jp">Japanese</a>
                    <a href="/another-valid"></a>
                </body>
            </html>
            `,
			expected: []string{
				"https://example.com/valid-link",
				"https://valid.com/path",
				"https://example.com/another-valid",
			},
		},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual, _, err := getURLsFromHTML(tc.inputBody, tc.inputURL)

			if err != nil {
				t.Errorf("Test %v - '%s' FAIL: unexpected error: %v", i, tc.name, err)
				return
			}

			// Convert slices to sets for commparison
			expectedSet := make(map[string]struct{})
			for _, e := range tc.expected {
				expectedSet[e] = struct{}{}
			}

			actualSet := make(map[string]struct{})
			for _, e := range actual {
				actualSet[e] = struct{}{}
			}

			result := reflect.DeepEqual(expectedSet, actualSet)

			if result == false {
				t.Errorf("Test %v - %s FAIL: expected URL: %v, actual: %v", i, tc.name, tc.expected, actual)
			}

		})
	}
}

func TestGetURLsFromHTMLRejectsOversizedTraversalReferences(t *testing.T) {
	oversized := "/" + strings.Repeat("a", utils.MaxURLBytesV1)
	tests := []struct {
		name string
		html string
	}{
		{name: "anchor href", html: fmt.Sprintf(`<a href=%q>link</a>`, oversized)},
		{name: "image src", html: fmt.Sprintf(`<img src=%q>`, oversized)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := getURLsFromHTML(test.html, "https://example.com/")
			assertHTMLDiscoveryComplexity(t, err, htmlDiscoveryURLAttributeLimit, utils.MaxURLBytesV1)
		})
	}
}

func TestGetURLsFromHTMLRejectsTooManyTokens(t *testing.T) {
	body := strings.Repeat("<b>", maxHTMLDiscoveryTokens+1)
	_, _, err := getURLsFromHTML(body, "https://example.com/")
	assertHTMLDiscoveryComplexity(t, err, htmlDiscoveryTokenLimit, maxHTMLDiscoveryTokens)
}

func TestGetURLsFromHTMLRejectsTooManyUniqueLinks(t *testing.T) {
	var body strings.Builder
	for index := 0; index <= maxUniqueOutgoingLinks; index++ {
		fmt.Fprintf(&body, `<a href="/%d">link</a>`, index)
	}

	_, _, err := getURLsFromHTML(body.String(), "https://example.com/")
	assertHTMLDiscoveryComplexity(t, err, htmlDiscoveryLinkLimit, maxUniqueOutgoingLinks)
}

func TestGetURLsFromHTMLRejectsTooManyUniqueImages(t *testing.T) {
	var body strings.Builder
	for index := 0; index <= maxUniqueImages; index++ {
		fmt.Fprintf(&body, `<img src="/%d.png" alt="image">`, index)
	}

	_, _, err := getURLsFromHTML(body.String(), "https://example.com/")
	assertHTMLDiscoveryComplexity(t, err, htmlDiscoveryImageLimit, maxUniqueImages)
}

func TestGetURLsFromHTMLRejectsOversizedRetainedAlt(t *testing.T) {
	body := fmt.Sprintf(`<img src="/image.png" alt=%q>`, strings.Repeat("a", maxRetainedImageAltBytes+1))
	_, _, err := getURLsFromHTML(body, "https://example.com/")
	assertHTMLDiscoveryComplexity(t, err, htmlDiscoveryAltLimit, maxRetainedImageAltBytes)
}

func TestGetURLsFromHTMLRetainsBoundedImageAltAndDeduplicatesImages(t *testing.T) {
	alt := strings.Repeat("a", maxRetainedImageAltBytes)
	body := fmt.Sprintf(
		`<img src="/image.png" alt="first"><img src="/image.png" alt=%q>`,
		alt,
	)
	_, images, err := getURLsFromHTML(body, "https://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || images["https://example.com/image.png"]["alt"] != alt {
		t.Fatalf("images = %#v", images)
	}
}

func TestValidateOutgoingLinkCountDefendsRedisBoundary(t *testing.T) {
	links := make([]string, maxUniqueOutgoingLinks+1)
	err := validateOutgoingLinkCount(links)
	assertHTMLDiscoveryComplexity(t, err, htmlDiscoveryLinkLimit, maxUniqueOutgoingLinks)
}

func assertHTMLDiscoveryComplexity(t *testing.T, err error, resource htmlDiscoveryLimit, limit int) {
	t.Helper()
	if !errors.Is(err, ErrHTMLDiscoveryComplexity) {
		t.Fatalf("error = %v, want ErrHTMLDiscoveryComplexity", err)
	}
	var complexityErr *HTMLDiscoveryComplexityError
	if !errors.As(err, &complexityErr) {
		t.Fatalf("error type = %T, want *HTMLDiscoveryComplexityError", err)
	}
	if complexityErr.Resource != resource || complexityErr.Limit != limit {
		t.Fatalf("complexity error = %#v, want resource=%s limit=%d", complexityErr, resource, limit)
	}
}
