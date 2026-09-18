package crawljobsv2

import (
	"bytes"
	"crypto/sha1" // #nosec G505 -- normative Redis script identity, not authorization.
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const canonicalBundleCaseName = "canonical-lua-bundle"
const canonicalLuaPackagePath = "services/spider/internal/database/crawljobsv2"

type canonicalBundleEntryResult struct {
	Index        int    `json:"index"`
	Operation    string `json:"operation"`
	SourceName   string `json:"source_name"`
	SourceBytes  int    `json:"source_bytes"`
	RedisSHA1    string `json:"redis_sha1"`
	SourceSHA256 string `json:"source_sha256"`
}

type canonicalBundleCaseResult struct {
	Entries          []canonicalBundleEntryResult `json:"entries"`
	LuaSourceOrder   []string                     `json:"lua_source_order"`
	SourceSetSHA256  string                       `json:"source_set_sha256"`
	ContractSHA256   string                       `json:"contract_sha256"`
	BundleSealSHA256 string                       `json:"bundle_seal_sha256"`
}

func canonicalDiskPath(t testing.TB, relative string) string {
	t.Helper()
	path := fixtureRepositoryRoot(t)
	for _, part := range strings.Split(relative, "/") {
		if part == "" || part == "." || part == ".." {
			t.Fatal("noncanonical test repository path")
		}
		path = filepath.Join(path, part)
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("canonical path absent or symlink: %s (%v)", relative, err)
		}
	}
	return path
}

// No expected digest is fed back into bundle construction. Compare the embedded
// complete bundle to the actual closed disk inventory first, then independently
// recompute all three identities from those exact bytes and the real document.
func canonicalDiskBundle(t testing.TB) (ScriptBindingSet, []byte) {
	t.Helper()
	set, err := AuthoritativeScriptBindingSet()
	if err != nil {
		t.Fatal(err)
	}
	packagePath := canonicalDiskPath(t, canonicalLuaPackagePath)
	canonicalDiskPath(t, canonicalLuaPackagePath+"/lua")
	disk, err := authoritativeScriptBindingSetFromFS(os.DirFS(packagePath))
	if err != nil {
		t.Fatalf("canonical on-disk bundle differs from compiled pins: %v; regenerate explicitly", err)
	}
	expectations := wireOracleOperationExpectations() // literal 43-op oracle, NOT the production table
	if len(expectations) != 43 || len(set.bindings) != 43 {
		t.Fatal("canonical bundle must contain the complete independent 43-operation inventory")
	}
	for index, expectation := range expectations {
		binding := set.bindings[index]
		canonicalDiskPath(t, canonicalLuaPackagePath+"/lua/"+expectation.scriptSource)
		if binding.operation != expectation.operation || binding.sourceName != expectation.scriptSource || binding != disk.bindings[index] {
			t.Fatalf("embedded/disk/literal-oracle identity mismatch at protocol index %d", index)
		}
	}
	document, err := os.ReadFile(canonicalDiskPath(t, "docs/crawl-jobs-v2.md"))
	if err != nil {
		t.Fatal(err)
	}
	return set, document
}

func canonicalBundleResultFromBytes(t testing.TB, document []byte, bindings []scriptBinding) canonicalBundleCaseResult {
	t.Helper()
	result := canonicalBundleCaseResult{Entries: make([]canonicalBundleEntryResult, 0, len(bindings))}
	hash := sha256.New()
	_, _ = hash.Write(F([]byte("mifolyo:crawl-jobs-v2:lua-source-set:v1")))
	_, _ = hash.Write(F([]byte(strconv.Itoa(len(bindings)))))
	for index, binding := range bindings {
		source := []byte(binding.source)
		redis := sha1.Sum(source) // #nosec G401 -- normative Redis identity.
		identity := canonicalBundleEntryResult{
			Index: index, Operation: string(binding.operation), SourceName: binding.sourceName,
			SourceBytes: len(source), RedisSHA1: hex.EncodeToString(redis[:]), SourceSHA256: fixtureSHA256(source),
		}
		result.Entries = append(result.Entries, identity)
		for _, value := range []string{strconv.Itoa(index), identity.Operation, identity.SourceName, binding.source, identity.RedisSHA1, identity.SourceSHA256} {
			_, _ = hash.Write(F([]byte(value)))
		}
	}
	result.SourceSetSHA256 = hex.EncodeToString(hash.Sum(nil))
	ordered := append([]scriptBinding(nil), bindings...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].sourceName < ordered[j].sourceName })
	result.LuaSourceOrder = make([]string, len(ordered))
	records := make([]Record, len(ordered))
	for index, binding := range ordered {
		result.LuaSourceOrder[index] = binding.sourceName
		records[index] = Record{textField("source_name", binding.sourceName), textField("source_bytes", binding.source)}
	}
	documentSection, err := EncodeSection("document", []Record{{{Name: "document_bytes", Value: document}}})
	if err != nil {
		t.Fatal(err)
	}
	luaSection, err := EncodeSection("lua", records)
	if err != nil {
		t.Fatal(err)
	}
	result.ContractSHA256 = string(digestEncoded("mifolyo:crawl-contract:v2", documentSection, luaSection))
	result.BundleSealSHA256 = fixtureSHA256(bytes.Join([][]byte{
		F([]byte("mifolyo:crawl-jobs-v2:lua-bundle-seal:v1")), F([]byte(result.SourceSetSHA256)), F([]byte(result.ContractSHA256)),
	}, nil))
	return result
}

func verifyCanonicalLuaBundleCase(t testing.TB, vector digestVectorCase) canonicalBundleCaseResult {
	t.Helper()
	if vector.Name != canonicalBundleCaseName || vector.Kind != "canonical_lua_bundle" {
		t.Fatal("not the canonical real-file bundle case")
	}
	fixtureRawObject(t, vector.Input, vector.Name+".input", "document_path", "lua_directory")
	input := decodeVectorPart[struct {
		DocumentPath string `json:"document_path"`
		LuaDirectory string `json:"lua_directory"`
	}](t, vector.Input, vector.Name+".input")
	if input.DocumentPath != "docs/crawl-jobs-v2.md" || input.LuaDirectory != canonicalLuaPackagePath+"/lua" {
		t.Fatal("canonical bundle fixture cannot choose source/document paths")
	}
	object := fixtureRawObject(t, vector.Expected, vector.Name+".expected", "entries", "lua_source_order", "source_set_sha256", "contract_sha256", "bundle_seal_sha256")
	for _, raw := range decodeVectorPart[[]json.RawMessage](t, object["entries"], vector.Name+".expected.entries") {
		entry := fixtureRawObject(t, raw, vector.Name+".expected.entry", "index", "operation", "source_name", "source_bytes", "redis_sha1", "source_sha256")
		for _, value := range entry {
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				t.Fatal("null canonical bundle identity field")
			}
		}
	}
	expected := decodeVectorPart[canonicalBundleCaseResult](t, vector.Expected, vector.Name+".expected")
	set, document := canonicalDiskBundle(t)
	actual := canonicalBundleResultFromBytes(t, document, set.bindings)
	assertVectorValue(t, actual, expected)
	if actual.SourceSetSHA256 != string(set.sourceSetSHA256) || actual.ContractSHA256 != string(set.approvedContractSHA256) ||
		actual.BundleSealSHA256 != string(set.seal.bundleSHA256) {
		t.Fatal("recomputed real-file contract/source-set/seal differs from generated Go literals")
	}
	return actual
}

func TestCanonicalBundleConformance(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	harness := newFixtureConformanceHarness(t, fixture)
	actual := verifyCanonicalLuaBundleCase(t, harness.fixtureCaseByName(t, canonicalBundleCaseName))
	guards := 0
	for _, vector := range fixture.Cases {
		if vector.Kind == "guard_core" || vector.Kind == "guard_chain" {
			var contractCase string
			if vector.Kind == "guard_chain" {
				contractCase = decodeVectorPart[fixtureGuardChainInput](t, vector.Input, vector.Name).ContractCase
			} else {
				contractCase = decodeVectorPart[fixtureGuardCoreCaseInput](t, vector.Input, vector.Name).ContractCase
			}
			if contractCase != canonicalBundleCaseName {
				t.Fatal("current guard/compatibility/provisional chain is not bound independently to the canonical contract")
			}
			guards++
		}
	}
	if guards != 2 {
		t.Fatalf("canonical guard chains = %d, want production and provisional", guards)
	}
	primitive := verifyContractDigestCase(t, harness.fixtureCaseByName(t, "contract-current-document-empty-lua"))
	if len(primitive.LuaSourceOrder) != 0 || primitive.ContractSHA256 == actual.ContractSHA256 {
		t.Fatal("empty-Lua primitive became canonical authority")
	}
}

func TestCanonicalBundlePreservesFoundationInventory(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	legacy := fixture
	legacy.Cases = nil
	legacy.NegativeVectors = nil
	for _, vector := range fixture.Cases {
		if vector.Name != canonicalBundleCaseName {
			legacy.Cases = append(legacy.Cases, vector)
		}
	}
	for _, vector := range fixture.NegativeVectors {
		if vector.Kind != "canonical_lua_bundle_mutation" {
			legacy.NegativeVectors = append(legacy.NegativeVectors, vector)
		}
	}
	if len(legacy.Cases) != 39 || len(legacy.NegativeVectors) != 139 ||
		digestFixtureInventory(legacy) != "56797748de64aa57618104bb5135d0300c9d192f41995e743ddb319219a248df" {
		t.Fatal("canonical extension replaced/reordered a foundation positive or negative case")
	}
}

func (h *fixtureConformanceHarness) canonicalLuaBundleMutationRejection(t *testing.T, vector digestVectorNegative) string {
	t.Helper()
	input := decodeVectorPart[struct {
		BaseCase string `json:"base_case"`
		Mutation string `json:"mutation"`
	}](t, vector.Input, vector.Name+".input")
	if input.BaseCase != canonicalBundleCaseName || h.fixtureCaseByName(t, input.BaseCase).Kind != "canonical_lua_bundle" {
		t.Fatal("bundle negative requires actual canonical bundle")
	}
	set, document := canonicalDiskBundle(t)
	other := Digest(strings.Repeat("1", 64))
	var err error
	switch input.Mutation {
	case "missing_source":
		set.bindings = set.bindings[:42]
	case "extra_source":
		set.bindings = append(set.bindings, set.bindings[0])
	case "swapped_sources":
		set.bindings[0].source, set.bindings[1].source = set.bindings[1].source, set.bindings[0].source
	case "duplicate_sources":
		set.bindings[1].source = set.bindings[0].source
	case "duplicate_entry":
		set.bindings[1] = set.bindings[0]
	case "swapped_entries":
		set.bindings[0], set.bindings[1] = set.bindings[1], set.bindings[0]
	case "source_alias_path":
		set.bindings[0].sourceName = "../lua/" + set.bindings[0].sourceName
	case "invalid_utf8":
		set.bindings[0].source = "\xff" + set.bindings[0].source[1:]
	case "missing_newline":
		source := set.bindings[0].source
		set.bindings[0].source = source[:len(source)-1]
	case "one_byte_drift", "source_resealed":
		source := []byte(set.bindings[0].source)
		source[0] ^= 1
		set.bindings[0].source = string(source)
		if input.Mutation == "source_resealed" {
			redis := sha1.Sum(source) // #nosec G401 -- normative Redis identity.
			set.bindings[0].redisSHA1 = hex.EncodeToString(redis[:])
			set.bindings[0].sourceSHA256 = Digest(fixtureSHA256(source))
			computed := canonicalBundleResultFromBytes(t, document, set.bindings)
			set.sourceSetSHA256 = Digest(computed.SourceSetSHA256)
			set.approvedContractSHA256 = Digest(computed.ContractSHA256)
			*set.seal = scriptBindingSetSeal{set.sourceSetSHA256, set.approvedContractSHA256, Digest(computed.BundleSealSHA256)}
			if err := set.validate(); err != nil {
				t.Fatal("resealed control must be internally self-consistent", err)
			}
		}
	case "stale_redis_sha1":
		set.bindings[0].redisSHA1 = strings.Repeat("1", 40)
	case "stale_source_sha256":
		set.bindings[0].sourceSHA256 = other
	case "source_set_substitution":
		set.sourceSetSHA256 = other
	case "bundle_seal_substitution":
		set.seal.bundleSHA256 = other
	case "contract_substitution", "contract_resealed":
		set.approvedContractSHA256 = other
		if input.Mutation == "contract_resealed" {
			set.seal.approvedContractSHA256 = other
			set.seal.bundleSHA256 = deriveScriptBindingSetSeal(set.sourceSetSHA256, other)
			if err := set.validate(); err != nil {
				t.Fatal("resealed control must be internally self-consistent", err)
			}
		}
	case "document_one_byte_drift":
		document[0] ^= 1
		computed := canonicalBundleResultFromBytes(t, document, set.bindings)
		if computed.ContractSHA256 != string(canonicalContractSHA256) {
			err = ErrScriptBindingMismatch
		}
	default:
		t.Fatalf("unknown canonical bundle mutation %q", input.Mutation)
	}
	if input.Mutation != "document_one_byte_drift" {
		err = validateCanonicalScriptBindingSet(set)
	}
	switch {
	case errors.Is(err, errCanonicalBundleInventory):
		return "BUNDLE_INVENTORY"
	case errors.Is(err, errCanonicalBundleBytes):
		return "BUNDLE_SOURCE_BYTES"
	case errors.Is(err, ErrScriptBindingMismatch):
		return "BUNDLE_IDENTITY_MISMATCH"
	default:
		t.Fatalf("canonical negative accepted or unexpected rejection: %v", err)
		return ""
	}
}

func newCanonicalWireOracleFixture(t *testing.T) *wireOracleFixture {
	t.Helper()
	set, document := canonicalDiskBundle(t)
	actual := canonicalBundleResultFromBytes(t, document, set.bindings)
	if actual.ContractSHA256 != string(set.approvedContractSHA256) {
		t.Fatal("oracle contract must be derived from actual canonical artifacts")
	}
	fixture := newWireOracleFixture(t)
	fixture.artifacts, fixture.retireInput = newWireOracleArtifactsForContract(t, fixture.runID, Digest(actual.ContractSHA256))
	fixture.semanticCanaries = newWireOracleSemanticCanaries(t, fixture)
	return fixture
}

func canonicalWireOracleBindings(t *testing.T, expectations []wireOracleOperationExpectation) (ScriptBindingSet, map[OperationName]wireOracleScriptIdentity) {
	t.Helper()
	set, _ := canonicalDiskBundle(t)
	identities := make(map[OperationName]wireOracleScriptIdentity, 43)
	for _, expectation := range expectations {
		source, err := os.ReadFile(canonicalDiskPath(t, canonicalLuaPackagePath+"/lua/"+expectation.scriptSource))
		if err != nil {
			t.Fatal(err)
		}
		redis := sha1.Sum(source) // #nosec G401 -- normative Redis identity.
		identities[expectation.operation] = wireOracleScriptIdentity{source, hex.EncodeToString(redis[:]), Digest(fixtureSHA256(source))}
	}
	return set, identities
}
