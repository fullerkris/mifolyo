// Command generate-unicode emits the dormant Lua URL validator's pinned data.
// Run from services/spider with Go 1.25.13, GOPROXY=off and GOSUMDB=off:
//
//	go run ./internal/database/crawljobsv2/tools/generate-unicode -work <private-temp-directory>
//
// -check verifies byte-identical regeneration without modifying the repository.
// -oracle writes the exhaustive, uncompressed Go property oracle to a temporary
// file for lua_url_test.go. No module-cache file is ever modified.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/idna"
	"golang.org/x/text/unicode/bidi"
	"golang.org/x/text/unicode/norm"
)

const identity = "cj2-url-v1/go1.25.13/x-net-v0.58.0/x-text-v0.41.0/unicode-15.0.0/data-1"

// These are hashes of the unmodified, selected upstream source bytes, not of
// their private instrumented copies. A different compiler/table is an error.
var pins = map[string]string{
	"net/idna/idna.go":                  "26de47c66ae70e53c5bedffca7fd61aefcde2ef8df6db37c9a50a3fb3c152aab",
	"net/idna/punycode.go":              "3e65858245f1a7d32e225aba714a305d3ef34496acb839956a7b15d1c7a34eb7",
	"net/idna/tables15.0.0.go":          "79fb7af83a29255478a5e56fdc8d176276ccdb4d1ff1b1459646cbf8d52cc5f3",
	"net/idna/trie.go":                  "8d2a16a56e4cab9d23a7afb49443bfe7a5ff0d74e885acc9121b4a9c598825eb",
	"net/idna/trieval.go":               "7e2c4893145e4b3b4d7984d70244af9c358da4973915085a42689ed1f3ce5159",
	"text/unicode/norm/composition.go":  "3d1be52960f2693926472819b747646e7d2371d2bb5f53097cf9d46c913052df",
	"text/unicode/norm/forminfo.go":     "844dadc7a0dc991a4b87a195699476a90655292d302abe2d2f7ddff5968f74bc",
	"text/unicode/norm/input.go":        "965b431790bb139543d71a9c497920ef7d9a15af417456a2bbd0cdb629330e8d",
	"text/unicode/norm/iter.go":         "4d580123776d78ff862131bd8c99fa5758f3cb70530619edc80ed4ee173cd83a",
	"text/unicode/norm/normalize.go":    "9e4fdc543d3b7aabf046ead80f2ef60746f579a7a4691c3c7d6bf69f4c4fa713",
	"text/unicode/norm/readwriter.go":   "d600d1f6cff2536dbb49a0f6bb8c06f6fc05f2754dad2dc05f1d44f9e8e48a35",
	"text/unicode/norm/tables15.0.0.go": "49dda94f9429bac8c29efe11e09558d46abfaa3c8e1de59816f2910b99cade04",
	"text/unicode/norm/transform.go":    "6f8014595643e2acae76d47ed6abe8ace969a0c9070dbfa5a89c86bebcec812d",
	"text/unicode/norm/trie.go":         "d87793d558251ee8824954f0b7bc5564803e4c9d59a8c4eebb4c3c5cfbd19492",
	"text/unicode/bidi/prop.go":         "f6391b2f69a1ae2a0ac1f0599a90b260ec350a19f73ad5d6572660100b5950bc",
	"text/unicode/bidi/tables15.0.0.go": "98c2d6d57e116667e73a4f5c89ee20ead7b27b50d561b5c5d23d8ebf0e3f3310",
	"text/unicode/bidi/trieval.go":      "bf3e17fca178c7d13139aa7cb7828005a0a1b8f4dca8ef97966236c6469a2136",
	"text/secure/bidirule/bidirule.go":  "714b5baaa1ec84a50e03504a7100d54d37e6ea7551d57d28265576c57de94ba9",
	"net/LICENSE":                       "911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad",
	"text/LICENSE":                      "911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad",
	"text/PATENTS":                      "96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc",
}

type license struct{ Name, URL, SHA256 string }

var licenses = []license{
	{"UNICODE-15-ReadMe.txt", "https://www.unicode.org/Public/15.0.0/ucd/ReadMe.txt", "53672c0d0b5185e3cf04c8e970d544c3af81ae7c8eeba0b9cf6d355aa954ae1f"},
	{"UNICODE-LICENSE.txt", "https://www.unicode.org/license.txt", "e7a93b009565cfce55919a381437ac4db883e9da2126fa28b91d12732bc53d96"},
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func read(p string) []byte { b, err := os.ReadFile(p); must(err); return b }
func write(p string, b []byte) {
	must(os.MkdirAll(filepath.Dir(p), 0755))
	must(os.WriteFile(p, b, 0644))
}
func run(dir string, args ...string) []byte {
	c := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "GOWORK=off")
	var stderr bytes.Buffer
	c.Stderr = &stderr
	b, err := c.Output()
	if err != nil {
		panic(fmt.Sprintf("go %v: %v\n%s", args, err, stderr.Bytes()))
	}
	return b
}

func main() {
	work := flag.String("work", "", "existing private temporary parent directory (required)")
	check := flag.Bool("check", false, "verify exact outputs; do not write repository files")
	oracle := flag.String("oracle", "", "write exhaustive raw Go properties to this temporary file")
	acquire := flag.Bool("acquire-licenses", false, "explicit network-only acquisition of the two authoritative notices")
	flag.Parse()
	if runtime.Version() != "go1.25.13" || unicode.Version != "15.0.0" || idna.UnicodeVersion != "15.0.0" || norm.Version != "15.0.0" || bidi.UnicodeVersion != "15.0.0" {
		panic("wrong compiler or Unicode tables")
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		panic("no build identity")
	}
	modules := map[string]string{"golang.org/x/net": "v0.58.0", "golang.org/x/text": "v0.41.0"}
	sums := map[string]string{"golang.org/x/net": "h1:ynWG7rqYi4ccpTEuPZ2QGWHktVEM9DMCj9yzDE0Q7To=", "golang.org/x/text": "h1:vz/seA0lnX87Othu2f/0L24RcgrXD9/YFTSuGjj3rH8="}
	seen := 0
	for _, d := range info.Deps {
		if v, ok := modules[d.Path]; ok {
			if d.Version != v || d.Sum != sums[d.Path] || d.Replace != nil {
				panic("wrong module identity")
			}
			seen++
		}
	}
	if seen != 2 {
		panic("missing pinned modules")
	}
	root, err := os.Getwd()
	must(err)
	out := filepath.Join(root, "internal/database/crawljobsv2/lua_src")
	if _, err := os.Stat(filepath.Join(out, "primitives.lua")); err != nil {
		panic("run from services/spider")
	}
	if *acquire {
		if *check || *oracle != "" {
			panic("acquisition cannot be combined with generation/check")
		}
		client := &http.Client{Timeout: 30 * time.Second}
		for _, l := range licenses {
			resp, err := client.Get(l.URL)
			must(err)
			if resp.StatusCode != 200 {
				panic(resp.Status)
			}
			b, err := io.ReadAll(io.LimitReader(resp.Body, 32769))
			resp.Body.Close()
			must(err)
			if len(b) > 32768 || len(b) == 0 {
				panic("notice size")
			}
			if l.SHA256 != "" && hash(b) != l.SHA256 {
				panic("remote notice changed; review required")
			}
			write(filepath.Join(out, "licenses", l.Name), b)
			fmt.Printf("%s %s\n", hash(b), l.Name)
		}
		return
	}
	if *work == "" {
		panic("-work required")
	}
	st, err := os.Stat(*work)
	must(err)
	if !st.IsDir() {
		panic("work is not a directory")
	}
	tmp, err := os.MkdirTemp(*work, "unicode-")
	must(err)
	defer os.RemoveAll(tmp)
	cache := strings.TrimSpace(string(run(root, "env", "GOMODCACHE")))
	sources := map[string][]byte{}
	for name, want := range pins {
		parts := strings.SplitN(name, "/", 2)
		version := modules["golang.org/x/"+parts[0]]
		b := read(filepath.Join(cache, "golang.org/x/"+parts[0]+"@"+version, parts[1]))
		if hash(b) != want {
			panic("source pin mismatch: " + name)
		}
		sources[name] = b
		pkg := ""
		if strings.HasPrefix(name, "net/idna/") {
			pkg = "idna"
		}
		if strings.HasPrefix(name, "text/unicode/norm/") {
			pkg = "norm"
		}
		if pkg != "" {
			b = bytes.ReplaceAll(b, []byte("package "+pkg+" // import \"golang.org/x/"+parts[0]+"/"+strings.TrimSuffix(parts[1], "/"+filepath.Base(name))+"\""), []byte("package "+pkg))
			write(filepath.Join(tmp, pkg, filepath.Base(name)), b)
		}
	}
	write(filepath.Join(tmp, "go.mod"), []byte("module cj2unicode\n\ngo 1.25.0\nrequire golang.org/x/text v0.41.0\n"))
	write(filepath.Join(tmp, "go.sum"), read(filepath.Join(root, "go.sum")))
	write(filepath.Join(tmp, "idna", "cj2_export.go"), []byte(idnaExport))
	write(filepath.Join(tmp, "norm", "cj2_export.go"), []byte(normExport))
	write(filepath.Join(tmp, "main.go"), []byte(oracleMain))
	raw := run(tmp, "run", "-mod=readonly", ".")
	if *oracle != "" {
		write(*oracle, raw)
	}
	lua, counts := generate(raw)
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		panic("generator source")
	}
	notices := map[string]string{"Go-BSD-3-Clause.txt": pins["net/LICENSE"], "Go-PATENTS.txt": pins["text/PATENTS"]}
	for _, l := range licenses {
		if l.SHA256 == "" || hash(read(filepath.Join(out, "licenses", l.Name))) != l.SHA256 {
			panic("unreviewed license bytes: " + l.Name)
		}
		notices[l.Name] = l.SHA256
	}
	provenance := map[string]any{
		"identity": identity, "toolchain": runtime.Version(), "unicode_version": unicode.Version,
		"module_versions": modules, "module_sums": sums, "source_sha256": pins,
		"generator_sha256": hash(read(self)), "unicode_data_lua_sha256": hash(lua),
		"exhaustive_go_oracle_sha256": hash(raw), "counts": counts,
		"licenses_sha256": notices, "unicode_notice_sources": licenses,
		"profile": []string{"ValidateForRegistration", "MapForLookup", "Transitional(false)", "StrictDomainName(true)", "ValidateLabels(true)", "CheckHyphens(true)", "CheckJoiners(true)", "BidiRule", "VerifyDNSLength(true)"},
		"notes":   []string{"Unicode 15.0.0 selected by !go1.27; stdlib unicode16 branch is false", "No ContextO; preserve byte-indexed hyphens, exact ContextJ DFA and stream-safe NFC", "Unicode release notice is dated 2022; license.txt is the separately retrieved current authoritative license, not a reconstructed historical notice", "Dormant support; final operation assembly must bind these exact bytes in its source identity"},
	}
	manifest, err := json.MarshalIndent(provenance, "", "  ")
	must(err)
	manifest = append(manifest, '\n')
	outputs := map[string][]byte{"unicode_data.lua": lua, "unicode-provenance.json": manifest, "licenses/Go-BSD-3-Clause.txt": sources["net/LICENSE"], "licenses/Go-PATENTS.txt": sources["text/PATENTS"]}
	for name, b := range outputs {
		p := filepath.Join(out, name)
		if *check {
			if !bytes.Equal(read(p), b) {
				panic("generated output drift: " + name)
			}
		} else {
			write(p, b)
		}
	}
	fmt.Printf("Unicode data verified: %v; lua bytes=%d sha256=%s\n", counts, len(lua), hash(lua))
}

// The private adapters use the real unexported upstream lookup/interpretation
// functions. No hand-copied category allowlist or normalization property reader
// participates in generation. The exhaustive stream is also the test oracle.
const idnaExport = `package idna
func CJ2Info(r rune) byte {
 if r >= 0xd800 && r <= 0xdfff { return 0 }
 v, _ := trie.lookupString(string(r)); x := info(v)
 p := New(ValidateForRegistration(), MapForLookup(), Transitional(false), StrictDomainName(true), ValidateLabels(true), CheckHyphens(true), CheckJoiners(true), BidiRule(), VerifyDNSLength(true))
 var f byte
 if c := p.simplify(x.category()); c == valid || c == deviation { f = 1 }
 if x.isModifier() { f |= 2 }; if x.isViramaModifier() { f |= 4 }
 return f | byte(x.joinType()) << 3
}
`
const normExport = `package norm
func CJ2Info(r rune) ([]byte, []rune) {
 p := NFC.PropertiesString(string(r))
 return []byte{p.ccc, byte(p.flags), p.nLead}, []rune(string(p.Decomposition()))
}
func CJ2Composition() string { return recompMapPacked }
`
const oracleMain = `package main
import ("bufio"; "encoding/binary"; "os"; "cj2unicode/idna"; "cj2unicode/norm"; "golang.org/x/text/unicode/bidi")
func main() {
 w := bufio.NewWriter(os.Stdout)
 for r := rune(0); r <= 0x10ffff; r++ {
  p, d := norm.CJ2Info(r); b, _ := bidi.LookupRune(r)
  w.Write([]byte{idna.CJ2Info(r), byte(b.Class()), p[0], p[1], p[2], byte(len(d))})
  for _, c := range d { binary.Write(w, binary.BigEndian, uint32(c)) }
 }
 s := norm.CJ2Composition(); binary.Write(w, binary.BigEndian, uint32(len(s))); w.WriteString(s)
 if err := w.Flush(); err != nil { panic(err) }
}
`

func put(b *[]byte, n, width int) {
	for i := width - 1; i >= 0; i-- {
		*b = append(*b, byte(n>>(8*i)))
	}
}
func get(b []byte, p, width int) int {
	n := 0
	for _, x := range b[p : p+width] {
		n = n*256 + int(x)
	}
	return n
}

func generate(raw []byte) ([]byte, map[string]int) {
	var props, ranges, decomps, ascii []byte
	ids := map[string]int{}
	dids := map[string]int{}
	all := make([]int, 0x110000)
	p, previous, maxDecomp := 0, -1, 0
	for cp := 0; cp < len(all); cp++ {
		if p+6 > len(raw) {
			panic("short oracle")
		}
		head := raw[p : p+6]
		p += 6
		n := int(head[5])
		if n > maxDecomp {
			maxDecomp = n
		}
		var d []byte
		for j := 0; j < n; j++ {
			put(&d, get(raw, p, 4), 3)
			p += 4
		}
		offset, ok := dids[string(d)]
		if !ok {
			offset = len(decomps)
			dids[string(d)] = offset
			decomps = append(decomps, d...)
		}
		r := append([]byte{}, head[:5]...)
		put(&r, offset, 3)
		r = append(r, byte(n))
		id, ok := ids[string(r)]
		if !ok {
			id = len(props) / 9
			ids[string(r)] = id
			props = append(props, r...)
		}
		if id > 65535 || offset > 0xffffff {
			panic("packed field overflow")
		}
		all[cp] = id
		if cp < 128 {
			put(&ascii, id, 2)
		}
		if id != previous {
			put(&ranges, cp, 3)
			put(&ranges, id, 2)
			previous = id
		}
	}
	// Verify the compressed lookup for EVERY code point, not just range edges.
	for cp, want := range all {
		lo, hi := 0, len(ranges)/5
		for lo+1 < hi {
			m := (lo + hi) / 2
			if get(ranges, m*5, 3) <= cp {
				lo = m
			} else {
				hi = m
			}
		}
		if get(ranges, lo*5+3, 2) != want {
			panic("exhaustive compression mismatch")
		}
	}
	n := get(raw, p, 4)
	p += 4
	if p+n != len(raw) || n%8 != 0 {
		panic("composition oracle framing")
	}
	type pair struct{ key, value int }
	var pairs []pair
	for ; p < len(raw); p += 8 {
		pairs = append(pairs, pair{get(raw, p, 4), get(raw, p+4, 4)})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].key < pairs[j].key })
	var compositions []byte
	for i, pair := range pairs {
		if i > 0 && pairs[i-1].key == pair.key {
			panic("duplicate composition key")
		}
		put(&compositions, pair.key, 4)
		put(&compositions, pair.value, 3)
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "-- Generated by tools/generate-unicode; DO NOT EDIT.\n-- Go Authors BSD-3-Clause; Unicode notices: licenses/.\n-- Exact source provenance: unicode-provenance.json. No runtime loaders.\nlocal byte, floor = string.byte, math.floor\n")
	for _, item := range []struct {
		name  string
		value []byte
	}{{"ranges", ranges}, {"props", props}, {"decomp", decomps}, {"compositions", compositions}, {"ascii", ascii}} {
		fmt.Fprintf(&b, "local %s =\n", item.name)
		for start := 0; start < len(item.value); start += 4096 {
			end := start + 4096
			if end > len(item.value) {
				end = len(item.value)
			}
			b.WriteString("    \"")
			for _, x := range item.value[start:end] {
				fmt.Fprintf(&b, "\\%03d", x)
			}
			b.WriteString("\"")
			if end < len(item.value) {
				b.WriteString(" ..")
			}
			b.WriteByte('\n')
		}
	}
	fmt.Fprintf(&b, dataFunctions, identity, maxDecomp)
	return b.Bytes(), map[string]int{"code_points_checked": len(all), "property_records": len(props) / 9, "ranges": len(ranges) / 5, "decomposition_bytes": len(decomps), "composition_pairs": len(pairs), "max_decomposition_scalars": maxDecomp, "packed_bytes": len(props) + len(ranges) + len(decomps) + len(compositions) + len(ascii)}
}

const dataFunctions = `local function uint(s, p, n)
    local v = 0
    for i = p, p + n - 1 do v = v * 256 + byte(s, i) end
    return v
end
local function get(cp)
    if type(cp) ~= "number" or cp < 0 or cp > 1114111 or cp ~= floor(cp) then return nil end
    if cp < 128 then return uint(ascii, cp * 2 + 1, 2) * 9 + 1 end
    local lo, hi = 0, #ranges / 5
    while lo + 1 < hi do
        local m = floor((lo + hi) / 2)
        if uint(ranges, m * 5 + 1, 3) <= cp then lo = m else hi = m end
    end
    return uint(ranges, lo * 5 + 4, 2) * 9 + 1
end
local function compose(a, b)
    -- Exactly x/text/norm's uint16-truncated recomposition key, NOT a new map.
    local key = (a %% 65536) * 65536 + b %% 65536
    local lo, hi = 0, #compositions / 7
    while lo < hi do
        local m = floor((lo + hi) / 2)
        local k = uint(compositions, m * 7 + 1, 4)
        if k == key then return uint(compositions, m * 7 + 5, 3) end
        if k < key then lo = m + 1 else hi = m end
    end
    return 0
end
return {identity = %q, max_decomposition = %d,
        get = get, compose = compose, props = props, decomp = decomp}
`
