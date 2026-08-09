package utils

import "testing"

func TestStripURLUsesV1CanonicalizationWithoutLoss(t *testing.T) {
	input := "HTTPS://www.Example.com:443/path/?version=1.0&language=en#fragment"
	expected := "https://www.example.com/path/?version=1.0&language=en"

	actual, err := StripURL(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if actual != expected {
		t.Errorf("got %q, want %q", actual, expected)
	}
}
