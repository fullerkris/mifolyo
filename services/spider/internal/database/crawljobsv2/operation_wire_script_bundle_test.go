package crawljobsv2

import (
	"crypto/sha1" // #nosec G505 -- Redis SCRIPT LOAD/EVALSHA identity is normatively SHA-1.
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

const testScriptBundleContractSHA256 Digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type testScriptSource struct {
	operation  OperationName
	sourceName string
	source     []byte
}

// newTestScriptBindingSet is the only arbitrary-source bundle factory. Because
// it lives in _test.go, it cannot become an execution trust path in production
// builds. Production deliberately has no equivalent factory until generated,
// reviewed Lua sources and their approved contract binding are added.
func newTestScriptBindingSet(t testing.TB) ScriptBindingSet {
	t.Helper()
	sources := make([]testScriptSource, len(operationWireOrder))
	for index, operation := range operationWireOrder {
		name := canonicalScriptSourceName(operation)
		sources[index] = testScriptSource{
			operation:  operation,
			sourceName: name,
			source:     []byte("-- " + name + "\nreturn {'OK'}\n"),
		}
	}
	return newTestScriptBindingSetFromSources(t, sources, testScriptBundleContractSHA256)
}

func newTestScriptBindingSetFromSources(
	t testing.TB,
	sources []testScriptSource,
	approvedContractSHA256 Digest,
) ScriptBindingSet {
	t.Helper()
	bindings := make([]scriptBinding, len(sources))
	for index, source := range sources {
		redisDigest := sha1.Sum(source.source) // #nosec G401 -- required by Redis EVALSHA.
		sourceDigest := sha256.Sum256(source.source)
		bindings[index] = scriptBinding{
			operation:    source.operation,
			sourceName:   source.sourceName,
			source:       string(source.source),
			redisSHA1:    hex.EncodeToString(redisDigest[:]),
			sourceSHA256: Digest(hex.EncodeToString(sourceDigest[:])),
		}
	}
	sourceSetSHA256 := deriveScriptSourceSetDigest(bindings)
	set := ScriptBindingSet{
		bindings:               bindings,
		sourceSetSHA256:        sourceSetSHA256,
		approvedContractSHA256: approvedContractSHA256,
		seal: &scriptBindingSetSeal{
			sourceSetSHA256:        sourceSetSHA256,
			approvedContractSHA256: approvedContractSHA256,
			bundleSHA256:           deriveScriptBindingSetSeal(sourceSetSHA256, approvedContractSHA256),
		},
	}
	if err := set.validate(); err != nil {
		t.Fatalf("construct test-only script bundle: %v", err)
	}
	return set
}

func cloneTestScriptBindingSet(set ScriptBindingSet) ScriptBindingSet {
	cloned := set
	cloned.bindings = append([]scriptBinding(nil), set.bindings...)
	if set.seal != nil {
		seal := *set.seal
		cloned.seal = &seal
	}
	return cloned
}

func testScriptBindingIndex(t testing.TB, operation OperationName) int {
	t.Helper()
	for index, candidate := range operationWireOrder {
		if candidate == operation {
			return index
		}
	}
	t.Fatalf("unknown test script operation %q", operation)
	return -1
}

func TestScriptBundleRejectsTrustAnchorMutations(t *testing.T) {
	valid := newTestScriptBindingSet(t)
	otherDigest := Digest(strings.Repeat("b", 64))
	tests := []struct {
		name   string
		want   error
		mutate func(*ScriptBindingSet)
	}{
		{name: "zero", want: ErrInvalidScriptBindingSet, mutate: func(set *ScriptBindingSet) { *set = ScriptBindingSet{} }},
		{name: "unsealed", want: ErrInvalidScriptBindingSet, mutate: func(set *ScriptBindingSet) { set.seal = nil }},
		{name: "omitted", want: ErrInvalidScriptBindingSet, mutate: func(set *ScriptBindingSet) {
			set.bindings = set.bindings[:len(set.bindings)-1]
		}},
		{name: "extra", want: ErrInvalidScriptBindingSet, mutate: func(set *ScriptBindingSet) {
			set.bindings = append(set.bindings, set.bindings[0])
		}},
		{name: "duplicate operation", want: ErrInvalidScriptBindingSet, mutate: func(set *ScriptBindingSet) {
			set.bindings[1].operation = set.bindings[0].operation
		}},
		{name: "renamed source", want: ErrInvalidScriptBindingSet, mutate: func(set *ScriptBindingSet) {
			set.bindings[0].sourceName = "renamed.lua"
		}},
		{name: "empty source", want: ErrInvalidScriptBindingSet, mutate: func(set *ScriptBindingSet) {
			set.bindings[0].source = ""
		}},
		{name: "swapped bindings", want: ErrInvalidScriptBindingSet, mutate: func(set *ScriptBindingSet) {
			set.bindings[0], set.bindings[1] = set.bindings[1], set.bindings[0]
		}},
		{name: "modified source", want: ErrScriptBindingMismatch, mutate: func(set *ScriptBindingSet) {
			set.bindings[0].source += "-- modified\n"
		}},
		{name: "modified redis hash", want: ErrScriptBindingMismatch, mutate: func(set *ScriptBindingSet) {
			set.bindings[0].redisSHA1 = strings.Repeat("0", 40)
		}},
		{name: "modified source hash", want: ErrScriptBindingMismatch, mutate: func(set *ScriptBindingSet) {
			set.bindings[0].sourceSHA256 = otherDigest
		}},
		{name: "swapped sources and hashes", want: ErrScriptBindingMismatch, mutate: func(set *ScriptBindingSet) {
			set.bindings[0].source, set.bindings[1].source = set.bindings[1].source, set.bindings[0].source
			set.bindings[0].redisSHA1, set.bindings[1].redisSHA1 = set.bindings[1].redisSHA1, set.bindings[0].redisSHA1
			set.bindings[0].sourceSHA256, set.bindings[1].sourceSHA256 = set.bindings[1].sourceSHA256, set.bindings[0].sourceSHA256
		}},
		{name: "duplicate source and hashes", want: ErrInvalidScriptBindingSet, mutate: func(set *ScriptBindingSet) {
			set.bindings[1].source = set.bindings[0].source
			set.bindings[1].redisSHA1 = set.bindings[0].redisSHA1
			set.bindings[1].sourceSHA256 = set.bindings[0].sourceSHA256
		}},
		{name: "source set digest", want: ErrScriptBindingMismatch, mutate: func(set *ScriptBindingSet) {
			set.sourceSetSHA256 = otherDigest
		}},
		{name: "approved contract", want: ErrScriptBindingMismatch, mutate: func(set *ScriptBindingSet) {
			set.approvedContractSHA256 = otherDigest
		}},
		{name: "sealed source set", want: ErrScriptBindingMismatch, mutate: func(set *ScriptBindingSet) {
			set.seal.sourceSetSHA256 = otherDigest
		}},
		{name: "sealed contract", want: ErrScriptBindingMismatch, mutate: func(set *ScriptBindingSet) {
			set.seal.approvedContractSHA256 = otherDigest
		}},
		{name: "bundle seal digest", want: ErrScriptBindingMismatch, mutate: func(set *ScriptBindingSet) {
			set.seal.bundleSHA256 = otherDigest
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneTestScriptBindingSet(valid)
			test.mutate(&candidate)
			if err := candidate.validate(); !errors.Is(err, test.want) {
				t.Fatalf("mutated bundle error = %v, want %v", err, test.want)
			}
		})
	}
}
