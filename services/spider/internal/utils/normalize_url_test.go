package utils

import "testing"

func TestNormalizeURLReturnsCanonicalAbsoluteURL(t *testing.T) {
	tests := []struct {
		name     string
		inputURL string
		expected string
		wantErr  bool
	}{
		{
			name:     "preserve scheme and path",
			inputURL: "https://en.wikipedia.org/wiki/Mega_Man_X",
			expected: "https://en.wikipedia.org/wiki/Mega_Man_X",
		},
		{
			name:     "preserve HTTP identity",
			inputURL: "http://en.wikipedia.org/wiki/Mega_Man_X",
			expected: "http://en.wikipedia.org/wiki/Mega_Man_X",
		},
		{
			name:     "preserve trailing slash and query",
			inputURL: "https://www.mults.com/path/?b=2&a=1#fragment",
			expected: "https://www.mults.com/path/?b=2&a=1",
		},
		{
			name:     "invalid scheme",
			inputURL: "ftp://www.mults.com/",
			wantErr:  true,
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := NormalizeURL(test.inputURL)
			if (err != nil) != test.wantErr {
				t.Fatalf("Test %v - %q: error = %v, wantErr = %v", index, test.name, err, test.wantErr)
			}
			if actual != test.expected {
				t.Errorf("Test %v - %q: got %q, want %q", index, test.name, actual, test.expected)
			}
		})
	}
}
