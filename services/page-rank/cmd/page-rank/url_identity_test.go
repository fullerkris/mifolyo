package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type canonicalizationContract struct {
	Version     int `json:"version"`
	MaxURLBytes int `json:"max_url_bytes"`
	Valid       []struct {
		Name         string `json:"name"`
		Input        string `json:"input"`
		CanonicalURL string `json:"canonical_url"`
	} `json:"valid"`
	Invalid []struct {
		Name  string `json:"name"`
		Input string `json:"input"`
	} `json:"invalid"`
}

func TestCanonicalizeURLV1SharedContract(t *testing.T) {
	contractPath := filepath.Join("..", "..", "..", "..", "contracts", "url-canonicalization", "v1.json")
	data, err := os.ReadFile(contractPath)
	if os.IsNotExist(err) {
		t.Skip("repository contract is outside the service-only Docker build context")
	}
	if err != nil {
		t.Fatalf("read canonicalization contract: %v", err)
	}

	var contract canonicalizationContract
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatalf("decode canonicalization contract: %v", err)
	}
	if contract.Version != canonicalizationVersion || contract.MaxURLBytes != maxURLBytesV1 {
		t.Fatalf("contract metadata does not match implementation: %#v", contract)
	}

	for _, fixture := range contract.Valid {
		t.Run("valid/"+fixture.Name, func(t *testing.T) {
			canonical, err := canonicalizeURLV1(fixture.Input)
			if err != nil {
				t.Fatalf("canonicalizeURLV1() error = %v", err)
			}
			if canonical != fixture.CanonicalURL {
				t.Fatalf("canonical URL = %q, want %q", canonical, fixture.CanonicalURL)
			}
			if err := validateURLIdentity(fixture.CanonicalURL); err != nil {
				t.Fatalf("validateURLIdentity(canonical) error = %v", err)
			}
			if fixture.Input != fixture.CanonicalURL && validateURLIdentity(fixture.Input) == nil {
				t.Fatalf("validateURLIdentity() accepted noncanonical input %q", fixture.Input)
			}
		})
	}
	for _, fixture := range contract.Invalid {
		t.Run("invalid/"+fixture.Name, func(t *testing.T) {
			if _, err := canonicalizeURLV1(fixture.Input); err == nil {
				t.Fatalf("canonicalizeURLV1() accepted invalid input %q", fixture.Input)
			}
		})
	}
}

func TestCanonicalizeURLV1ServiceContextCoverage(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "HTTPS://Example.COM:443", want: "https://example.com/"},
		{input: "https://example.com/%7euser", want: "https://example.com/%7Euser"},
		{input: "https://example.com/straße", want: "https://example.com/stra%C3%9Fe"},
		{input: "https://xn--fa-hia.de/", want: "https://xn--fa-hia.de/"},
	}
	for _, test := range tests {
		canonical, err := canonicalizeURLV1(test.input)
		if err != nil {
			t.Fatalf("canonicalizeURLV1(%q) error = %v", test.input, err)
		}
		if canonical != test.want {
			t.Fatalf("canonicalizeURLV1(%q) = %q, want %q", test.input, canonical, test.want)
		}
	}
}
