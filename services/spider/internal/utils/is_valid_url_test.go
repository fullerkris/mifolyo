package utils

import "testing"

func TestIsValidURL(t *testing.T) {
	tests := []struct {
		name     string
		inputURL string
		expected bool
	}{
		{name: "absolute URL", inputURL: "https://en.wikipedia.org/wiki/Mega_Man_X", expected: true},
		{name: "relative URL", inputURL: "en.wikipedia.org/wiki/Mega_Man_X", expected: false},
		{name: "Unicode path", inputURL: "https://ja.wikipedia.org/wiki/仮面ライダーシリーズ", expected: true},
		{name: "Unicode hostname", inputURL: "https://пример.рф/path", expected: false},
		{name: "IP literal", inputURL: "http://127.0.0.1/path", expected: false},
		{name: "non-default port", inputURL: "https://example.com:8443/path", expected: false},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := IsValidURL(test.inputURL)
			if actual != test.expected {
				t.Errorf("Test %v - %q: got %v, want %v", index, test.name, actual, test.expected)
			}
		})
	}
}
