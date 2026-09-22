package crawljobsv2

import (
	"crypto/sha1" // #nosec G505 -- Redis SCRIPT LOAD/EVALSHA identity is normatively SHA-1.
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// These tests use only the existing package-local synthetic closed-bundle
// factories. They exercise identity, not canonical M3 Lua operation acceptance.
func TestScriptLoadIdentityPreservesRetryAndDefensiveCopies(t *testing.T) {
	bundle := newTestScriptBindingSet(t)
	retry, err := BuildEvalSHARequest(bundle, newMaintainWireRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	wantRetry := retry
	wantRetry.keys, wantRetry.arguments = retry.Keys(), retry.Arguments()
	plan, err := prepareScriptLoad(bundle, retry)
	if err != nil {
		t.Fatal(err)
	}
	index := testScriptBindingIndex(t, retry.Operation())
	wantCommand := [][]byte{[]byte("SCRIPT"), []byte("LOAD"), []byte(bundle.bindings[index].source)}
	command := plan.commandArguments()
	if !reflect.DeepEqual(command, wantCommand) {
		t.Fatal("SCRIPT LOAD did not retain the exact sealed source")
	}
	for _, part := range command {
		part[0] = 'X'
	}
	command[2] = []byte("replacement source")
	keys, arguments := retry.Keys(), retry.Arguments()
	keys[0][0], arguments[0][0] = 'X', 'X'
	if !reflect.DeepEqual(retry, wantRetry) {
		t.Fatal("retry accessors exposed mutable wire data")
	}
	// Even package-local mutation of the input cannot rewrite the saved plan.
	retry.keys[0][0], retry.arguments[0][0] = 'Y', 'Y'
	retry.keys[1], retry.arguments[1] = []byte("replacement key"), []byte("replacement argument")
	bundle.bindings[index].source = "replacement source"
	bundle.seal.bundleSHA256 = Digest(strings.Repeat("b", 64))
	if !reflect.DeepEqual(plan.commandArguments(), wantCommand) {
		t.Fatal("SCRIPT LOAD command bytes were aliased")
	}
	authority := newTransportAuthority()
	loaded, err := authority.verifyScriptLoad(plan, wantRetry.ScriptSHA1())
	if err != nil || !reflect.DeepEqual(loaded, wantRetry) {
		t.Fatalf("reload changed EVALSHA identity, KEYS, ARGV, size, or provenance: %v", err)
	}
	keys, arguments = loaded.Keys(), loaded.Arguments()
	keys[0][0], arguments[0][0] = 'X', 'X'
	if !reflect.DeepEqual(loaded, wantRetry) {
		t.Fatal("returned retry accessors exposed mutable wire data")
	}
	loaded.keys[0][0], loaded.arguments[0][0] = 'Z', 'Z'
	loaded.keys[1], loaded.arguments[1] = nil, nil
	again, err := authority.verifyScriptLoad(plan, wantRetry.ScriptSHA1())
	if err != nil || !reflect.DeepEqual(again, wantRetry) {
		t.Fatalf("verification mutated or exposed the saved retry: %v", err)
	}
}

func TestScriptLoadIdentityRejectsOtherBundleWithSameSelectedSource(t *testing.T) {
	for _, changed := range []string{"other source", "approved contract"} {
		t.Run(changed, func(t *testing.T) {
			bundle := newTestScriptBindingSet(t)
			retry, err := BuildEvalSHARequest(bundle, newMaintainWireRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := prepareScriptLoad(cloneTestScriptBindingSet(bundle), retry); err != nil {
				t.Fatalf("copy of the same complete bundle identity was rejected: %v", err)
			}
			sources := make([]testScriptSource, len(bundle.bindings))
			for index, binding := range bundle.bindings {
				sources[index] = testScriptSource{binding.operation, binding.sourceName, []byte(binding.source)}
			}
			contract := bundle.approvedContractSHA256
			if changed == "other source" {
				sources[0].source = append(sources[0].source, []byte("-- different unselected source\n")...)
			} else {
				contract = Digest(strings.Repeat("b", 64))
			}
			other := newTestScriptBindingSetFromSources(t, sources, contract)
			selected := testScriptBindingIndex(t, retry.Operation())
			if other.bindings[selected] != bundle.bindings[selected] || *other.seal == *bundle.seal {
				t.Fatal("test requires identical selected source and different complete bundle identity")
			}
			if plan, err := prepareScriptLoad(other, retry); !errors.Is(err, ErrScriptBindingMismatch) || plan.commandArguments() != nil {
				t.Fatalf("other complete bundle was not rejected: %v", err)
			}
			// Provenance is a value snapshot, not a mutable alias to the old seal.
			*bundle.seal = *other.seal
			other.seal = bundle.seal
			if _, err := prepareScriptLoad(other, retry); !errors.Is(err, ErrScriptBindingMismatch) {
				t.Fatalf("mutating the originating seal changed retry provenance: %v", err)
			}
		})
	}
}

func TestScriptLoadIdentityRevalidatesCompleteBundleAndRequest(t *testing.T) {
	bundle := newTestScriptBindingSet(t)
	retry, err := BuildEvalSHARequest(bundle, newMaintainWireRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	selected := testScriptBindingIndex(t, retry.Operation())
	tests := []struct {
		name   string
		want   error
		mutate func(*ScriptBindingSet, *EvalSHARequest)
	}{
		{name: "zero bundle", want: ErrInvalidScriptBindingSet, mutate: func(set *ScriptBindingSet, _ *EvalSHARequest) { *set = ScriptBindingSet{} }},
		{name: "unsealed bundle", want: ErrInvalidScriptBindingSet, mutate: func(set *ScriptBindingSet, _ *EvalSHARequest) { set.seal = nil }},
		{name: "partial bundle with selected source", want: ErrInvalidScriptBindingSet, mutate: func(set *ScriptBindingSet, _ *EvalSHARequest) {
			set.bindings = set.bindings[selected : selected+1]
		}},
		{name: "changed selected source", want: ErrScriptBindingMismatch, mutate: func(set *ScriptBindingSet, _ *EvalSHARequest) { set.bindings[selected].source += "\n" }},
		{name: "changed unselected source", want: ErrScriptBindingMismatch, mutate: func(set *ScriptBindingSet, _ *EvalSHARequest) { set.bindings[0].source += "\n" }},
		{name: "zero retry", want: ErrScriptBindingMismatch, mutate: func(_ *ScriptBindingSet, request *EvalSHARequest) { *request = EvalSHARequest{} }},
		{name: "missing provenance", want: ErrScriptBindingMismatch, mutate: func(_ *ScriptBindingSet, request *EvalSHARequest) { request.bundleSeal = scriptBindingSetSeal{} }},
		{name: "wrong operation", want: ErrScriptBindingMismatch, mutate: func(_ *ScriptBindingSet, request *EvalSHARequest) { request.operation = OperationApproveBoot }},
		{name: "wrong name", want: ErrScriptBindingMismatch, mutate: func(_ *ScriptBindingSet, request *EvalSHARequest) { request.scriptName = "other.lua" }},
		{name: "wrong SHA1", want: ErrScriptBindingMismatch, mutate: func(_ *ScriptBindingSet, request *EvalSHARequest) { request.scriptSHA1 = strings.Repeat("b", 40) }},
		{name: "wrong SHA256", want: ErrScriptBindingMismatch, mutate: func(_ *ScriptBindingSet, request *EvalSHARequest) {
			request.sourceSHA256 = Digest(strings.Repeat("b", 64))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate, request := cloneTestScriptBindingSet(bundle), retry
			test.mutate(&candidate, &request)
			plan, err := prepareScriptLoad(candidate, request)
			if !errors.Is(err, test.want) || plan.commandArguments() != nil {
				t.Fatalf("invalid load preparation did not fail closed: %v", err)
			}
		})
	}
}

func TestScriptLoadIdentityRehashesSourceSHA256BeforePreparation(t *testing.T) {
	bundle := newTestScriptBindingSet(t)
	retry, err := BuildEvalSHARequest(bundle, newMaintainWireRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	selected := testScriptBindingIndex(t, retry.Operation())
	for _, operation := range []OperationName{retry.Operation(), OperationApproveBoot} {
		t.Run(string(operation), func(t *testing.T) {
			candidate, request := cloneTestScriptBindingSet(bundle), retry
			binding := &candidate.bindings[testScriptBindingIndex(t, operation)]
			binding.source += "-- modified source\n"
			redisDigest := sha1.Sum([]byte(binding.source)) // #nosec G401 -- required by Redis EVALSHA.
			binding.redisSHA1 = hex.EncodeToString(redisDigest[:])
			// Keep only sourceSHA256 stale. Test-local resealing and matching retry
			// provenance isolate the source rehash from the other identity checks.
			candidate.sourceSetSHA256 = deriveScriptSourceSetDigest(candidate.bindings)
			candidate.seal.sourceSetSHA256 = candidate.sourceSetSHA256
			candidate.seal.bundleSHA256 = deriveScriptBindingSetSeal(candidate.sourceSetSHA256, candidate.approvedContractSHA256)
			request.bundleSeal = *candidate.seal
			request.scriptSHA1 = candidate.bindings[selected].redisSHA1
			plan, err := prepareScriptLoad(candidate, request)
			if !errors.Is(err, ErrScriptBindingMismatch) || plan.commandArguments() != nil {
				t.Fatalf("source SHA256 was not rechecked before preparation: %v", err)
			}
		})
	}
}

func TestScriptLoadIdentityRequiresUTF8EvenWithSelfConsistentHashes(t *testing.T) {
	bundle := newTestScriptBindingSet(t)
	wire := newMaintainWireRequest(t)
	retry, err := BuildEvalSHARequest(bundle, wire)
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []OperationName{retry.Operation(), OperationApproveBoot} {
		t.Run(string(operation), func(t *testing.T) {
			candidate := cloneTestScriptBindingSet(bundle)
			binding := &candidate.bindings[testScriptBindingIndex(t, operation)]
			binding.source += "-- invalid UTF-8: \xff\n"
			redisDigest := sha1.Sum([]byte(binding.source)) // #nosec G401 -- required by Redis EVALSHA.
			sourceDigest := sha256.Sum256([]byte(binding.source))
			binding.redisSHA1 = hex.EncodeToString(redisDigest[:])
			binding.sourceSHA256 = Digest(hex.EncodeToString(sourceDigest[:]))
			candidate.sourceSetSHA256 = deriveScriptSourceSetDigest(candidate.bindings)
			candidate.seal.sourceSetSHA256 = candidate.sourceSetSHA256
			candidate.seal.bundleSHA256 = deriveScriptBindingSetSeal(candidate.sourceSetSHA256, candidate.approvedContractSHA256)
			if err := candidate.validate(); !errors.Is(err, ErrInvalidScriptBindingSet) {
				t.Fatalf("self-consistent hashes admitted invalid UTF-8 source: %v", err)
			}
			if _, err := BuildEvalSHARequest(candidate, wire); !errors.Is(err, ErrInvalidScriptBindingSet) {
				t.Fatalf("request builder admitted invalid UTF-8 source: %v", err)
			}
			if plan, err := prepareScriptLoad(candidate, retry); !errors.Is(err, ErrInvalidScriptBindingSet) || plan.commandArguments() != nil {
				t.Fatalf("reload admitted invalid UTF-8 source: %v", err)
			}
		})
	}
}

func TestScriptLoadIdentityPreservesUTF8SourceBytes(t *testing.T) {
	bundle := newTestScriptBindingSet(t)
	sources := make([]testScriptSource, len(bundle.bindings))
	for index, binding := range bundle.bindings {
		sources[index] = testScriptSource{binding.operation, binding.sourceName, []byte(binding.source)}
	}
	selected := testScriptBindingIndex(t, OperationMaintainRateScopes)
	sources[selected].source = append(sources[selected].source, []byte("-- π 雪\r\n")...)
	bundle = newTestScriptBindingSetFromSources(t, sources, bundle.approvedContractSHA256)
	retry, err := BuildEvalSHARequest(bundle, newMaintainWireRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := prepareScriptLoad(bundle, retry)
	if err != nil || !reflect.DeepEqual(plan.commandArguments(), [][]byte{[]byte("SCRIPT"), []byte("LOAD"), sources[selected].source}) {
		t.Fatalf("valid UTF-8 source bytes were rejected or normalized: %v", err)
	}
}

func TestScriptLoadIdentityAcceptsOnlyExactLowercaseSHA1String(t *testing.T) {
	bundle := newTestScriptBindingSet(t)
	retry, err := BuildEvalSHARequest(bundle, newMaintainWireRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := prepareScriptLoad(bundle, retry)
	if err != nil {
		t.Fatal(err)
	}
	type namedString string
	sha := retry.ScriptSHA1()
	for _, test := range []struct {
		name string
		raw  any
	}{
		{"nil", nil}, {"bytes", []byte(sha)}, {"integer", int64(1)}, {"float", float64(1)}, {"bool", true},
		{"string array", []string{sha}}, {"RESP array", []any{sha}}, {"named string", namedString(sha)},
		{"pointer", &sha}, {"error", errors.New(sha)}, {"empty", ""},
		{"uppercase", strings.ToUpper(sha)}, {"short", sha[:39]}, {"long", sha + "0"},
		{"leading space", " " + sha}, {"trailing data", sha + "artifact"}, {"CRLF", sha + "\r\n"},
		{"NUL", sha + "\x00"}, {"nonhex", strings.Repeat("g", 40)}, {"wrong hash", strings.Repeat("b", 40)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := newTransportAuthority().verifyScriptLoad(plan, test.raw)
			if !errors.Is(err, errScriptLoadReply) || !reflect.DeepEqual(got, EvalSHARequest{}) {
				t.Fatalf("invalid reply did not fail closed with the stable error: %v", err)
			}
		})
	}
	got, err := newTransportAuthority().verifyScriptLoad(plan, sha)
	if err != nil || !reflect.DeepEqual(got, retry) {
		t.Fatalf("exact load reply did not return the saved retry: %v", err)
	}
}

func TestScriptLoadIdentityCapabilityIsNotFreshConnectionValidation(t *testing.T) {
	bundle := newTestScriptBindingSet(t)
	retry, err := BuildEvalSHARequest(bundle, newMaintainWireRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := prepareScriptLoad(bundle, retry)
	if err != nil {
		t.Fatal(err)
	}
	for _, authority := range []transportAuthority{{}, {seal: &transportAuthoritySeal{marker: 1}}, {requestIOSession: &requestIOAuthoritySession{knownUnused: true}}} {
		if got, err := authority.verifyScriptLoad(plan, retry.ScriptSHA1()); !errors.Is(err, ErrInvalidResponseAuthority) || !reflect.DeepEqual(got, EvalSHARequest{}) {
			t.Fatalf("untrusted capability admitted a load reply: %v", err)
		}
	}
	// No connection exists and no live boot/marker recheck has occurred. The
	// current capability can validate reply identity, not those M5 runtime facts.
	authority := newTransportAuthority()
	if authority.requestIOSession != nil {
		t.Fatal("test requires a capability without a runtime session")
	}
	if got, err := authority.verifyScriptLoad(plan, retry.ScriptSHA1()); err != nil || !reflect.DeepEqual(got, retry) {
		t.Fatalf("private capability could not verify reply identity: %v", err)
	}
	if got, err := authority.verifyScriptLoad(scriptLoadPlan{}, retry.ScriptSHA1()); !errors.Is(err, errInvalidScriptLoadPlan) || !reflect.DeepEqual(got, EvalSHARequest{}) {
		t.Fatalf("zero plan did not fail closed: %v", err)
	}
}

func TestScriptLoadIdentityRedactionSurfacesAndErrors(t *testing.T) {
	bundle := newTestScriptBindingSet(t)
	retry, err := BuildEvalSHARequest(bundle, newMaintainWireRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := prepareScriptLoad(bundle, retry)
	if err != nil {
		t.Fatal(err)
	}
	rawValues := []string{plan.source, retry.ScriptName(), retry.ScriptSHA1(), string(retry.SourceSHA256()),
		string(bundle.sourceSetSHA256), string(bundle.approvedContractSHA256), string(bundle.seal.bundleSHA256)}
	for _, value := range append(retry.Keys(), retry.Arguments()...) {
		if len(value) > 1 {
			rawValues = append(rawValues, string(value))
		}
	}
	for _, value := range []any{plan, &plan, scriptLoadPlan{}} {
		assertRedactionSurfaces(t, redactionSurfaceCase{
			name: "script load plan", typeName: "scriptLoadPlan", value: value, composite: true, rawValues: rawValues,
		})
	}
	assertRedactionSurfaces(t, redactionSurfaceCase{
		name: "retry with bundle provenance", typeName: "EvalSHARequest", value: retry, composite: true, rawValues: rawValues,
	})
	_, bundleErr := prepareScriptLoad(ScriptBindingSet{}, retry)
	_, retryErr := prepareScriptLoad(bundle, EvalSHARequest{})
	_, authorityErr := (transportAuthority{}).verifyScriptLoad(plan, retry.ScriptSHA1())
	_, planErr := newTransportAuthority().verifyScriptLoad(scriptLoadPlan{}, retry.ScriptSHA1())
	_, replyErr := newTransportAuthority().verifyScriptLoad(plan, errors.New(strings.Join(rawValues, " ")))
	_, hashErr := newTransportAuthority().verifyScriptLoad(plan, bundle.bindings[0].redisSHA1)
	rawValues = append(rawValues, bundle.bindings[0].redisSHA1)
	for _, err := range []error{bundleErr, retryErr, authorityErr, planErr, replyErr, hashErr} {
		if err == nil {
			t.Fatal("test requires a rejected load")
		}
		assertRawValuesAbsent(t, err.Error(), rawValues)
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
			assertRawValuesAbsent(t, fmt.Sprintf(format, err), rawValues)
		}
		for _, value := range []any{err, err.Error()} {
			encoded, marshalErr := json.Marshal(value)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			assertRawValuesAbsent(t, string(encoded), rawValues)
		}
	}
}
