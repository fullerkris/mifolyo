package crawljobsv2

import (
	"errors"
	"strings"
	"testing"
)

func TestDeriveOperationalReferenceContractVectors(t *testing.T) {
	key := operationalReferenceTestKey()
	tests := []struct {
		name       string
		kind       OperationalReferenceKind
		identifier string
		want       string
	}{
		{name: "run", kind: OperationalReferenceRun, identifier: "00112233445566778899aabbccddeeff", want: "1a51e560b78bba2572b2441241791f6e"},
		{name: "job", kind: OperationalReferenceJob, identifier: "e73f29cb21ebd90c5b8d31b650169d9154d3ab942bc3dd67a456fe9feddc5f9a", want: "93c474a0a2d70b2ae9644b7f7dbeeda8"},
		{name: "owner", kind: OperationalReferenceOwner, identifier: "ffeeddccbbaa99887766554433221100", want: "e3b0c85348fb6254858bf26c4a627e92"},
		{name: "group", kind: OperationalReferenceGroup, identifier: "group-redaction-canary", want: "0c48eb553667b967818fd819e3d39c4d"},
		{name: "origin", kind: OperationalReferenceOrigin, identifier: "https://example.com:443", want: "bcc4b6a80b66335e5ac2e101f58f8fa9"},
		{name: "derived scope", kind: OperationalReferenceScope, identifier: strings.Repeat("a", 64), want: "5aeb5a3019959f940dedfcf7d95a7d0b"},
		{name: "rate scope", kind: OperationalReferenceScope, identifier: strings.Repeat("a", 32), want: "36ee869603fdac673045798326a1781a"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reference, err := DeriveOperationalReference(key, test.kind, test.identifier)
			if err != nil {
				t.Fatalf("derive reference: %v", err)
			}
			encoded := string(reference)
			if encoded != test.want {
				t.Fatalf("reference = %q, want %q", encoded, test.want)
			}
			if len(encoded) != operationalReferenceBytes*2 || !isLowerHex(encoded, operationalReferenceBytes*2) {
				t.Fatalf("reference has invalid output shape: %q", encoded)
			}
			if strings.Contains(encoded, test.identifier) {
				t.Fatal("operational reference exposed its raw identifier")
			}
		})
	}
}

func TestDeriveOperationalReferenceValidatesKeyKindAndIdentifier(t *testing.T) {
	validKey := operationalReferenceTestKey()
	tests := []struct {
		name       string
		key        []byte
		kind       OperationalReferenceKind
		identifier string
		want       error
	}{
		{name: "nil key", kind: OperationalReferenceRun, identifier: strings.Repeat("1", 32), want: ErrInvalidOperationalReferenceKey},
		{name: "short key", key: make([]byte, MinOperationalReferenceKeyBytes-1), kind: OperationalReferenceRun, identifier: strings.Repeat("1", 32), want: ErrInvalidOperationalReferenceKey},
		{name: "empty kind", key: validKey, identifier: strings.Repeat("1", 32), want: ErrInvalidOperationalReferenceKind},
		{name: "unknown kind", key: validKey, kind: OperationalReferenceKind("RUN"), identifier: strings.Repeat("1", 32), want: ErrInvalidOperationalReferenceKind},
		{name: "invalid run", key: validKey, kind: OperationalReferenceRun, identifier: "RAW_RUN_IDENTIFIER_CANARY", want: ErrInvalidOperationalReferenceIdentifier},
		{name: "invalid job", key: validKey, kind: OperationalReferenceJob, identifier: strings.Repeat("1", 32), want: ErrInvalidOperationalReferenceIdentifier},
		{name: "invalid owner", key: validKey, kind: OperationalReferenceOwner, identifier: strings.Repeat("g", 32), want: ErrInvalidOperationalReferenceIdentifier},
		{name: "invalid group", key: validKey, kind: OperationalReferenceGroup, identifier: "bad\ngroup", want: ErrInvalidOperationalReferenceIdentifier},
		{name: "invalid origin", key: validKey, kind: OperationalReferenceOrigin, identifier: "https://example.com", want: ErrInvalidOperationalReferenceIdentifier},
		{name: "invalid scope", key: validKey, kind: OperationalReferenceScope, identifier: strings.Repeat("f", 33), want: ErrInvalidOperationalReferenceIdentifier},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reference, err := DeriveOperationalReference(test.key, test.kind, test.identifier)
			if reference != "" || !errors.Is(err, test.want) {
				t.Fatalf("reference = %q, error = %v, want %v", reference, err, test.want)
			}
			if test.identifier != "" && strings.Contains(err.Error(), test.identifier) {
				t.Fatal("validation error exposed the rejected raw identifier")
			}
		})
	}
}

func TestOperationalReferenceDomainAndDeploymentKeySeparation(t *testing.T) {
	identifier := "00112233445566778899aabbccddeeff"
	key := operationalReferenceTestKey()
	runReference, err := DeriveOperationalReference(key, OperationalReferenceRun, identifier)
	if err != nil {
		t.Fatalf("derive run reference: %v", err)
	}
	ownerReference, err := DeriveOperationalReference(key, OperationalReferenceOwner, identifier)
	if err != nil {
		t.Fatalf("derive owner reference: %v", err)
	}
	if runReference == ownerReference {
		t.Fatal("reference kind did not domain-separate equal raw identifiers")
	}

	otherKey := append([]byte(nil), key...)
	otherKey[0] ^= 0xff
	otherDeploymentReference, err := DeriveOperationalReference(otherKey, OperationalReferenceRun, identifier)
	if err != nil {
		t.Fatalf("derive alternate deployment reference: %v", err)
	}
	if runReference == otherDeploymentReference {
		t.Fatal("deployment key did not separate operational references")
	}
}

func operationalReferenceTestKey() []byte {
	key := make([]byte, MinOperationalReferenceKeyBytes)
	for index := range key {
		key[index] = byte(index)
	}
	return key
}
