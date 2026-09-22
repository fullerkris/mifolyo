package crawljobsv2

import (
	"bytes"
	"crypto/sha1" // #nosec G505 -- normative Redis script identity.
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func TestAuthoritativeScriptBindingSetDefensivePrivateLiterals(t *testing.T) {
	first, err := AuthoritativeScriptBindingSet()
	if err != nil {
		t.Fatal(err)
	}
	second, err := AuthoritativeScriptBindingSet()
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("stable complete bundle: %v", err)
	}
	if first.seal == second.seal || &first.bindings[0] == &second.bindings[0] {
		t.Fatal("factory returned mutable shared representation")
	}
	for _, typ := range []reflect.Type{reflect.TypeOf(first), reflect.TypeOf(*first.seal), reflect.TypeOf(first.bindings[0])} {
		for index := 0; index < typ.NumField(); index++ {
			if typ.Field(index).IsExported() {
				t.Fatal("bundle representation exposes authority fields")
			}
		}
	}
	first.bindings[0].source = "not canonical\n"
	first.seal.bundleSHA256 = Digest(strings.Repeat("f", 64))
	pins := canonicalScriptBindingPins()
	pins[0].redisSHA1 = strings.Repeat("f", 40)
	third, err := AuthoritativeScriptBindingSet()
	if err != nil || !reflect.DeepEqual(third, second) || third.validate() != nil {
		t.Fatalf("caller/test mutation escaped defensive bundle: %v", err)
	}
	digest, err := ContractSHA256()
	if err != nil || digest != second.approvedContractSHA256 || digest != canonicalContractSHA256 {
		t.Fatalf("validated contract pin differs: %v", err)
	}
}

func canonicalMapFS(t testing.TB) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	for _, expectation := range wireOracleOperationExpectations() {
		name := "lua/" + expectation.scriptSource
		source, err := authoritativeLuaFS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: source, Mode: 0444}
	}
	return files
}

type duplicateCanonicalDirFS struct{ fs.FS }

func (files duplicateCanonicalDirFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(files.FS, name)
	if err == nil && len(entries) > 1 {
		entries[1] = entries[0]
	}
	return entries, err
}

func TestCanonicalBundleClosedEmbeddedInventory(t *testing.T) {
	base := canonicalMapFS(t)
	if _, err := authoritativeScriptBindingSetFromFS(base); err != nil {
		t.Fatal("real source map rejected", err)
	}
	tests := []struct {
		name   string
		change func(fstest.MapFS)
		want   error
	}{
		{"missing", func(f fstest.MapFS) { delete(f, "lua/cj2_commit.lua") }, errCanonicalBundleInventory},
		{"extra", func(f fstest.MapFS) { f["lua/extra.lua"] = &fstest.MapFile{Data: []byte("extra\n")} }, errCanonicalBundleInventory},
		{"hidden", func(f fstest.MapFS) { f["lua/.hidden"] = &fstest.MapFile{Data: []byte("hidden\n")} }, errCanonicalBundleInventory},
		{"underscore", func(f fstest.MapFS) { f["lua/_hidden"] = &fstest.MapFile{Data: []byte("hidden\n")} }, errCanonicalBundleInventory},
		{"subdirectory", func(f fstest.MapFS) { f["lua/nested/source.lua"] = f["lua/cj2_commit.lua"] }, errCanonicalBundleInventory},
		{"empty_directory", func(f fstest.MapFS) { f["lua/empty"] = &fstest.MapFile{Mode: fs.ModeDir | 0755} }, errCanonicalBundleInventory},
		{"symlink", func(f fstest.MapFS) {
			f["lua/cj2_commit.lua"] = &fstest.MapFile{Mode: fs.ModeSymlink, Data: []byte("cj2_dead.lua")}
		}, errCanonicalBundleInventory},
		{"source_is_directory", func(f fstest.MapFS) { f["lua/cj2_commit.lua"] = &fstest.MapFile{Mode: fs.ModeDir | 0755} }, errCanonicalBundleInventory},
		{"uppercase_alias", func(f fstest.MapFS) {
			f["lua/CJ2_COMMIT.lua"] = f["lua/cj2_commit.lua"]
			delete(f, "lua/cj2_commit.lua")
		}, errCanonicalBundleInventory},
		{"source_alias", func(f fstest.MapFS) { f["lua/cj2_commit.lua"] = f["lua/cj2_dead.lua"] }, ErrScriptBindingMismatch},
		{"utf8", func(f fstest.MapFS) { f["lua/cj2_commit.lua"] = &fstest.MapFile{Data: []byte("\xff\n")} }, errCanonicalBundleBytes},
		{"newline", func(f fstest.MapFS) {
			b := f["lua/cj2_commit.lua"].Data
			f["lua/cj2_commit.lua"] = &fstest.MapFile{Data: b[:len(b)-1]}
		}, errCanonicalBundleBytes},
		{"empty", func(f fstest.MapFS) { f["lua/cj2_commit.lua"] = &fstest.MapFile{} }, errCanonicalBundleBytes},
		{"one_byte", func(f fstest.MapFS) {
			b := append([]byte(nil), f["lua/cj2_commit.lua"].Data...)
			b[0] ^= 1
			f["lua/cj2_commit.lua"] = &fstest.MapFile{Data: b}
		}, ErrScriptBindingMismatch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			files := fstest.MapFS{}
			for name, value := range base {
				files[name] = value
			}
			test.change(files)
			set, err := authoritativeScriptBindingSetFromFS(files)
			if !errors.Is(err, test.want) || len(set.bindings) != 0 || set.seal != nil {
				t.Fatalf("invalid inventory exposed partial authority: %v", err)
			}
		})
	}
	if _, err := authoritativeScriptBindingSetFromFS(duplicateCanonicalDirFS{base}); !errors.Is(err, errCanonicalBundleInventory) {
		t.Fatalf("duplicate directory entries not rejected: %v", err)
	}
}

func TestCanonicalBundleScriptLoadFailureBoundSourceAndRawRealBundleSHA(t *testing.T) {
	fixture := newCanonicalWireOracleFixture(t)
	bundle, err := AuthoritativeScriptBindingSet()
	if err != nil {
		t.Fatal(err)
	}
	gate := wireOracleGate(t, fixture, OperationMaintainRateScopes, wireOracleActive)
	request, err := wireOracleConstructRequest(fixture, OperationMaintainRateScopes, wireOracleActive, gate)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := BuildEvalSHARequest(bundle, request)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := prepareScriptLoad(bundle, retry)
	if err != nil {
		t.Fatal(err)
	}
	source, err := authoritativeLuaFS.ReadFile("lua/cj2_maintain_rate_scopes.lua")
	if err != nil {
		t.Fatal(err)
	}
	command := plan.commandArguments()
	if len(command) != 3 || string(command[0]) != "SCRIPT" || string(command[1]) != "LOAD" || !bytes.Equal(command[2], source) {
		t.Fatal("load plan did not bind exact real source")
	}
	realSHA := sha1.Sum(source) // #nosec G401 -- required Redis SCRIPT LOAD confirmation.
	rawLoadRealBundleSHA := hex.EncodeToString(realSHA[:])
	if rawLoadRealBundleSHA != retry.ScriptSHA1() {
		t.Fatal("real source SHA differs from retry")
	}
	// Private identity interpretation only. No connection, Redis, dispatch, new
	// public raw-reply factory, or claim of fresh boot/marker transport authority.
	authority := newTransportAuthority()
	loaded, err := authority.verifyScriptLoad(plan, rawLoadRealBundleSHA)
	if err != nil || !reflect.DeepEqual(loaded, retry) {
		t.Fatalf("real SHA confirmation changed retry: %v", err)
	}
	for _, bad := range []any{nil, []byte(rawLoadRealBundleSHA), strings.ToUpper(rawLoadRealBundleSHA), strings.Repeat("0", 40), rawLoadRealBundleSHA[:39]} {
		if result, err := authority.verifyScriptLoad(plan, bad); err == nil || !reflect.DeepEqual(result, EvalSHARequest{}) {
			t.Fatal("invalid SCRIPT LOAD confirmation released a retry")
		}
	}
	if _, err := (transportAuthority{}).verifyScriptLoad(plan, rawLoadRealBundleSHA); !errors.Is(err, ErrInvalidResponseAuthority) {
		t.Fatal("zero transport capability accepted", err)
	}
	command[2][0] ^= 1
	if !bytes.Equal(plan.commandArguments()[2], source) {
		t.Fatal("load bytes aliased")
	}
	// A changed unselected source still invalidates the complete load binding.
	bundle.bindings[0].source += "-- drift\n"
	failureBoundSource, err := prepareScriptLoad(bundle, retry)
	if err == nil || failureBoundSource.commandArguments() != nil {
		t.Fatal("failed complete binding retained source authority")
	}
	// Correctly formed gates for another (foundation/old) contract are not enough.
	oldFixture := newWireOracleFixture(t)
	oldGate := wireOracleGate(t, oldFixture, OperationMaintainRateScopes, wireOracleActive)
	oldRequest, err := wireOracleConstructRequest(oldFixture, OperationMaintainRateScopes, wireOracleActive, oldGate)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err = AuthoritativeScriptBindingSet()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildEvalSHARequest(bundle, oldRequest); !errors.Is(err, ErrScriptContractMismatch) {
		t.Fatal("substituted contract accepted", err)
	}
}

func writeCanonicalSandboxFile(t testing.TB, root, relative string, raw []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
}

func newCanonicalSandbox(t testing.TB) string {
	t.Helper()
	root := t.TempDir()
	for name, file := range canonicalMapFS(t) {
		writeCanonicalSandboxFile(t, root, canonicalLuaPackagePath+"/"+name, file.Data)
	}
	for _, relative := range []string{"scripts/generate-crawl-jobs-v2-bundle.py", "docs/crawl-jobs-v2.md",
		"contracts/crawl-jobs-v2/digest-vectors.json", canonicalLuaPackagePath + "/script_bundle_generated.go"} {
		raw, err := os.ReadFile(canonicalDiskPath(t, relative))
		if err != nil {
			t.Fatal(err)
		}
		writeCanonicalSandboxFile(t, root, relative, raw)
	}
	return root
}

func TestCanonicalBundleGeneratorClosedPathsAndStalePins(t *testing.T) {
	root := newCanonicalSandbox(t)
	script := filepath.Join(root, "scripts/generate-crawl-jobs-v2-bundle.py")
	run := func(t *testing.T, pass bool, args ...string) {
		t.Helper()
		cmd := exec.Command("python3", append([]string{"-B", script}, args...)...)
		cmd.Dir = t.TempDir() // cwd is not source authority
		output, err := cmd.CombinedOutput()
		if (err == nil) != pass {
			t.Fatalf("generator pass=%v error=%v\n%s", pass, err, output)
		}
	}
	run(t, true, "--check")
	mutateFile := func(t *testing.T, relative string, mutate func([]byte) []byte) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(relative))
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, mutate(append([]byte(nil), raw...)), 0644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.WriteFile(path, raw, 0644); err != nil {
				t.Error(err)
			}
		})
	}
	for _, name := range []string{"missing", "extra", "hidden", "subdirectory", "symlink", "symlink_directory", "duplicate_source", "invalid_utf8", "no_newline", "one_byte", "document_byte", "go_pin", "fixture_pin"} {
		t.Run(name, func(t *testing.T) {
			sourceRel := canonicalLuaPackagePath + "/lua/cj2_commit.lua"
			sourcePath := filepath.Join(root, filepath.FromSlash(sourceRel))
			switch name {
			case "missing", "symlink":
				raw, err := os.ReadFile(sourcePath)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(sourcePath); err != nil {
					t.Fatal(err)
				}
				if name == "symlink" {
					if err := os.Symlink("cj2_dead.lua", sourcePath); err != nil {
						t.Fatal(err)
					}
				}
				t.Cleanup(func() {
					if name == "symlink" {
						_ = os.Remove(sourcePath)
					}
					if err := os.WriteFile(sourcePath, raw, 0644); err != nil {
						t.Error(err)
					}
				})
			case "extra", "hidden", "subdirectory":
				relative := canonicalLuaPackagePath + "/lua/extra.lua"
				if name == "hidden" {
					relative = canonicalLuaPackagePath + "/lua/.hidden"
				}
				if name == "subdirectory" {
					relative = canonicalLuaPackagePath + "/lua/nested/extra.lua"
				}
				writeCanonicalSandboxFile(t, root, relative, []byte("not canonical\n"))
				t.Cleanup(func() {
					_ = os.Remove(filepath.Join(root, relative))
					if name == "subdirectory" {
						_ = os.Remove(filepath.Join(root, canonicalLuaPackagePath+"/lua/nested"))
					}
				})
			case "symlink_directory":
				directory := filepath.Join(root, canonicalLuaPackagePath+"/lua")
				if err := os.Rename(directory, directory+".saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("lua.saved", directory); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					_ = os.Remove(directory)
					if err := os.Rename(directory+".saved", directory); err != nil {
						t.Error(err)
					}
				})
			case "duplicate_source":
				raw, err := os.ReadFile(filepath.Join(root, canonicalLuaPackagePath+"/lua/cj2_dead.lua"))
				if err != nil {
					t.Fatal(err)
				}
				mutateFile(t, sourceRel, func([]byte) []byte { return raw })
			case "invalid_utf8":
				mutateFile(t, sourceRel, func(raw []byte) []byte { raw[0] = 255; return raw })
			case "no_newline":
				mutateFile(t, sourceRel, func(raw []byte) []byte { return raw[:len(raw)-1] })
			case "one_byte":
				mutateFile(t, sourceRel, func(raw []byte) []byte { raw[0] ^= 1; return raw })
			case "document_byte":
				mutateFile(t, "docs/crawl-jobs-v2.md", func(raw []byte) []byte { raw[0] ^= 1; return raw })
			case "go_pin":
				mutateFile(t, canonicalLuaPackagePath+"/script_bundle_generated.go", func(raw []byte) []byte {
					return bytes.Replace(raw, []byte(canonicalBundleSealSHA256), []byte(strings.Repeat("1", 64)), 1)
				})
			case "fixture_pin":
				mutateFile(t, "contracts/crawl-jobs-v2/digest-vectors.json", func(raw []byte) []byte {
					return bytes.Replace(raw, []byte(canonicalBundleSealSHA256), []byte(strings.Repeat("1", 64)), 1)
				})
			}
			pinsPath := filepath.Join(root, canonicalLuaPackagePath+"/script_bundle_generated.go")
			before, err := os.ReadFile(pinsPath)
			if err != nil {
				t.Fatal(err)
			}
			run(t, false, "--check")
			after, err := os.ReadFile(pinsPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("--check wrote pins", err)
			}
			if name == "missing" {
				if err := os.Remove(pinsPath); err != nil {
					t.Fatal(err)
				}
				run(t, false) // Even write mode cannot create usable partial pins or fill missing Lua.
				if _, err := os.Lstat(pinsPath); !os.IsNotExist(err) {
					t.Fatal("partial pins were generated")
				}
				if _, err := os.Lstat(sourcePath); !os.IsNotExist(err) {
					t.Fatal("missing canonical source was synthesized")
				}
				if err := os.WriteFile(pinsPath, before, 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	run(t, true, "--check")
	run(t, false, "--source", "attacker.lua")
	t.Run("complete_only_regeneration_is_deterministic", func(t *testing.T) {
		pinsPath := filepath.Join(root, canonicalLuaPackagePath+"/script_bundle_generated.go")
		before, err := os.ReadFile(pinsPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(pinsPath); err != nil {
			t.Fatal(err)
		}
		run(t, false, "--check")
		run(t, true)
		after, err := os.ReadFile(pinsPath)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("complete regeneration is not byte deterministic", err)
		}
		run(t, true, "--check")
	})
}

func TestCanonicalBundlePythonClosedInventory(t *testing.T) {
	// Exercise the independent Python reader on REAL file copies, including
	// filesystem shapes that cannot be represented as inline JSON source text.
	root := newCanonicalSandbox(t)
	verifier := filepath.Join(fixtureRepositoryRoot(t), "scripts/verify-crawl-jobs-v2-digests.py")
	code := `import copy, json, pathlib, runpy, sys
m = runpy.run_path(sys.argv[1], run_name="bundle_reader_test")
root = pathlib.Path(sys.argv[2])
directory = root / m["CANONICAL_LUA_DIRECTORY"]
read = m["_canonical_read_bundle"]
compute = m["_canonical_bundle_compute"]
baseline = compute(*read(root))
assert len(baseline["entries"]) == 43
source = directory / "cj2_commit.lua"
original = source.read_bytes()
def denied(label, expected):
    try:
        result = compute(*read(root))
        if result != baseline:
            m["reject"]("BUNDLE_IDENTITY_MISMATCH")
    except m["Rejection"] as error:
        assert error.rejection_class == expected, (label, error.rejection_class)
    else:
        raise AssertionError("accepted " + label)
for label in ("missing", "symlink", "duplicate", "utf8", "newline", "drift"):
    source.unlink()
    try:
        if label == "symlink": source.symlink_to("cj2_dead.lua")
        elif label == "duplicate": source.write_bytes((directory / "cj2_dead.lua").read_bytes())
        elif label == "utf8": source.write_bytes(b"\xff" + original[1:])
        elif label == "newline": source.write_bytes(original[:-1])
        elif label == "drift": source.write_bytes(bytes([original[0] ^ 1]) + original[1:])
        denied(label, "BUNDLE_INVENTORY" if label in ("missing", "symlink") else "BUNDLE_SOURCE_BYTES" if label in ("utf8", "newline") else "BUNDLE_IDENTITY_MISMATCH")
    finally:
        if source.exists() or source.is_symlink(): source.unlink()
        source.write_bytes(original)
for name in ("extra.lua", ".hidden", "_hidden", "nested"):
    entry = directory / name
    if name == "nested": entry.mkdir()
    else: entry.write_bytes(b"extra\n")
    try: denied(name, "BUNDLE_INVENTORY")
    finally:
        if entry.is_dir(): entry.rmdir()
        else: entry.unlink()
saved = directory.with_name("lua.saved")
directory.rename(saved)
directory.symlink_to(saved, target_is_directory=True)
try: denied("symlink directory", "BUNDLE_INVENTORY")
finally:
    directory.unlink()
    saved.rename(directory)
good_input = {"document_path": m["CANONICAL_DOCUMENT_PATH"], "lua_directory": m["CANONICAL_LUA_DIRECTORY"]}
for field in good_input:
    for alias in ("../" + good_input[field], "./" + good_input[field], "/" + good_input[field]):
        bad = dict(good_input); bad[field] = alias
        try: m["_canonical_bundle_input"](bad)
        except m["FixtureError"]: pass
        else: raise AssertionError("accepted caller path alias")
assert compute(*read(root)) == baseline
fixture = json.loads((root / "contracts/crawl-jobs-v2/digest-vectors.json").read_text())
case = next(case for case in fixture["cases"] if case["kind"] == "canonical_lua_bundle")
actual = m["canonical_lua_bundle_result"](case["input"])
assert not m["compare"](case["expected"], actual, "canonical")
for field in ("source_set_sha256", "contract_sha256", "bundle_seal_sha256"):
    bad = copy.deepcopy(case); bad["expected"][field] = "1" * 64
    assert m["compare"](bad["expected"], actual, "canonical"), field
for field in ("redis_sha1", "source_sha256", "source_bytes"):
    bad = copy.deepcopy(case)
    bad["expected"]["entries"][0][field] = 1 if field == "source_bytes" else "1" * (40 if field == "redis_sha1" else 64)
    assert m["compare"](bad["expected"], actual, "canonical"), field
for mutation in ("duplicate", "swap", "missing_index", "null_index", "caller_sources"):
    bad = copy.deepcopy(case)
    entries = bad["expected"]["entries"]
    if mutation == "duplicate": entries[1] = copy.deepcopy(entries[0])
    elif mutation == "swap": entries[0], entries[1] = entries[1], entries[0]
    elif mutation == "missing_index": del entries[0]["index"]
    elif mutation == "null_index": entries[0]["index"] = None
    else: bad["input"]["lua_sources"] = []
    try: m["validate_case_schema"](bad, "canonical")
    except m["FixtureError"]: pass
    else: raise AssertionError("accepted bad fixture " + mutation)
# Even evaluate_cases' historical root argument cannot select canonical bytes.
# A relocated fixture and a forged same-shaped source tree remain non-authority.
source.write_bytes(bytes([original[0] ^ 1]) + original[1:])
try:
    relocated = m["evaluate_cases"]([case], fixture, root)[case["name"]]
    assert relocated == actual
finally: source.write_bytes(original)
print("Python closed canonical inventory controls verified")
`
	cmd := exec.Command("python3", "-B", "-c", code, verifier, root)
	cmd.Dir = t.TempDir()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("independent Python inventory controls: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}
