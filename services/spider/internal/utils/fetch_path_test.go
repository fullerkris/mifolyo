package utils

import (
	"net/url"
	"testing"
)

func TestIsUnambiguousFetchPath(t *testing.T) {
	tests := []struct {
		name    string
		rawPath string
		allowed bool
	}{
		{name: "plain", rawPath: "/docs/page", allowed: true},
		{name: "canonical space", rawPath: "/docs/a%20page", allowed: true},
		{name: "canonical unicode", rawPath: "/docs/%E7%A7%98%E5%AF%86", allowed: true},
		{name: "encoded unreserved", rawPath: "/docs/%70rivate", allowed: false},
		{name: "encoded slash", rawPath: "/docs%2Fprivate", allowed: false},
		{name: "encoded backslash", rawPath: "/docs/%5Cprivate", allowed: false},
		{name: "repeated slash", rawPath: "/docs//private", allowed: false},
		{name: "dot segment", rawPath: "/docs/../private", allowed: false},
		{name: "encoded percent", rawPath: "/docs/%25literal", allowed: false},
		{name: "double encoded traversal", rawPath: "/docs/%252E%252E/private", allowed: false},
		{name: "invalid decoded UTF-8", rawPath: "/docs/%FF", allowed: false},
		{name: "overlong UTF-8 slash", rawPath: "/docs/%C0%AFprivate", allowed: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := url.Parse("https://example.com" + test.rawPath)
			if err != nil {
				t.Fatalf("url.Parse failed: %v", err)
			}
			if got := IsUnambiguousFetchPath(parsed); got != test.allowed {
				t.Fatalf("IsUnambiguousFetchPath(%q) = %t, want %t", test.rawPath, got, test.allowed)
			}
		})
	}

	if IsUnambiguousFetchPath(nil) {
		t.Fatal("nil URL was allowed")
	}
}
