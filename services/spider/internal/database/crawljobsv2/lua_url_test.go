package crawljobsv2

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
	lua "github.com/yuin/gopher-lua"
	"golang.org/x/net/idna"
	"golang.org/x/text/unicode/bidi"
	"golang.org/x/text/unicode/norm"
)

type urlLuaVM struct {
	*primitiveLuaVM
	url, data *lua.LTable
	factory   lua.LValue
}

func urlLuaNew(t *testing.T, internals bool) *urlLuaVM {
	t.Helper()
	if runtime.Version() != "go1.25.13" || unicode.Version != "15.0.0" || norm.Version != "15.0.0" || bidi.UnicodeVersion != "15.0.0" || idna.UnicodeVersion != "15.0.0" {
		t.Fatal("URL oracle requires pinned Go 1.25.13 and Unicode 15.0.0")
	}
	vm := primitiveLuaNew(t, true) // Includes this test's context and deadline.
	load := func(source []byte, name string, args ...lua.LValue) lua.LValue {
		fn, err := vm.state.Load(bytes.NewReader(source), name)
		if err != nil {
			t.Fatal(err)
		}
		vm.state.SetFEnv(fn, vm.env)
		if err := vm.state.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}, args...); err != nil {
			t.Fatal(err)
		}
		v := vm.state.Get(-1)
		vm.state.Pop(1)
		return v
	}
	d := load(primitiveLuaRead(t, "lua_src/unicode_data.lua"), "unicode_data.lua").(*lua.LTable)
	source := string(primitiveLuaRead(t, "lua_src/url.lua"))
	if internals {
		// Test-only lexical visibility, not a replacement implementation or Go
		// callback. All function bodies and the data chunk remain byte-identical.
		const ret = "return {check_canonical=check_canonical, derive_origin=derive_origin}"
		if strings.Count(source, ret) != 1 {
			t.Fatal("private test export anchor drift")
		}
		source = strings.Replace(source, ret, "return {check_canonical=check_canonical, derive_origin=derive_origin, nfc=nfc_normal, alabel=valid_alabel, decode=puny_decode, encode=puny_encode}", 1)
	}
	factory := load([]byte("return function(P,D)\n"+source+"\nend"), "url.lua factory")
	if err := vm.state.CallByParam(lua.P{Fn: factory, NRet: 1, Protect: true}, vm.module, d); err != nil {
		t.Fatal(err)
	}
	u := vm.state.Get(-1).(*lua.LTable)
	vm.state.Pop(1)
	return &urlLuaVM{vm, u, d, factory}
}

func (vm *urlLuaVM) call(t *testing.T, name string, args ...lua.LValue) (lua.LValue, lua.LValue) {
	t.Helper()
	return primitiveLuaInvoke(t, vm.primitiveLuaVM, vm.url.RawGetString(name), args...)
}

func urlLuaCompare(t *testing.T, vm *urlLuaVM, s string) bool {
	t.Helper()
	identity, err := utils.CanonicalizeURLV1(s)
	want := err == nil && identity.CanonicalURL == s
	v, failure := vm.call(t, "check_canonical", lua.LString(s), lua.LNumber(1))
	if (v != lua.LNil) != want {
		t.Fatalf("canonical parity %q: Lua=%v/%v Go=%+v/%v", s, v, failure, identity, err)
	}
	if !want {
		if failure != lua.LString("INVALID_IDENTIFIER") {
			t.Fatalf("unexpected closed rejection %v", failure)
		}
	} else {
		if failure != lua.LNil {
			t.Fatalf("success with failure %v", failure)
		}
		r := v.(*lua.LTable)
		p, err := url.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		port := 80
		if p.Scheme == "https" {
			port = 443
		}
		if p.Port() != "" {
			port, err = strconv.Atoi(p.Port())
			if err != nil {
				t.Fatal(err)
			}
		}
		fields := map[string]lua.LValue{
			"canonical_url": lua.LString(s), "url_id": lua.LString(utils.URLIDV1(s)),
			"scheme": lua.LString(p.Scheme), "host": lua.LString(p.Hostname()), "port": lua.LNumber(port),
			"explicit_port": lua.LBool(p.Port() != ""), "is_ip": lua.LBool(net.ParseIP(p.Hostname()) != nil),
		}
		if net.ParseIP(p.Hostname()) == nil {
			fields["origin"] = lua.LString(p.Scheme + "://" + p.Hostname() + ":" + strconv.Itoa(port))
		}
		for k, w := range fields {
			if r.RawGetString(k) != w {
				t.Fatalf("%q field %s: got %v want %v", s, k, r.RawGetString(k), w)
			}
		}
		count := 0
		r.ForEach(func(_, _ lua.LValue) { count++ })
		if count != len(fields) {
			t.Fatal("extra result fields")
		}
	}
	origin, originErr := DeriveCanonicalOrigin(s)
	got, fail := vm.call(t, "derive_origin", lua.LString(s))
	if originErr == nil {
		if got != lua.LString(origin) || fail != lua.LNil {
			t.Fatalf("origin parity %q: %v/%v want %q", s, got, fail, origin)
		}
	} else if got != lua.LNil || fail != lua.LString("INVALID_IDENTIFIER") {
		t.Fatalf("origin rejection %q: %v/%v", s, got, fail)
	}
	return want
}

func TestURLLuaCanonicalMatrix(t *testing.T) {
	t.Parallel()
	vm := urlLuaNew(t, false)
	// Explicit tags for the shared URL canonical output, without importing the
	// normalizer's private fixture harness or changing any shared fixture.
	var shared struct {
		Valid []struct {
			Input     string `json:"input"`
			Canonical string `json:"canonical_url"`
		}
		Invalid []struct {
			Input string `json:"input"`
		}
	}
	b, err := os.ReadFile("../../utils/testdata/url-canonicalization-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &shared); err != nil {
		t.Fatal(err)
	}
	for _, v := range shared.Valid {
		urlLuaCompare(t, vm, v.Input)
		urlLuaCompare(t, vm, v.Canonical)
	}
	for _, v := range shared.Invalid {
		urlLuaCompare(t, vm, v.Input)
	}
	for _, s := range []string{
		"", "https://", "https:///a", "https://a", "https://a?", "https://a/?", "https://a/??",
		"http://localhost/", "https://127.1/", "http://2130706433/", "https://0x7f.0.0.1/",
		"https://ab--cd.example/", "https://a./", "https://a..b/", "https://-a.b/", "https://a-.b/",
		"https://bad_host.com/", "https://%65xample.com/", "https://%C3%A9.com/", "https://@a/",
		"https://a/a/../b//./?x=1&x=&x=2;+x=Y", "https://a/@user:pass?foo@bar",
		"https://a/%FF", "https://a/%80", "https://a/%ED%A0%80", "https://a/%C0%AF",
		"https://a/%2500", "https://a/%5C", "https://a/%C2%2585", "https://a/%C2x%85",
		"https://a/%C2%85", "https://a/%", "https://a/%0", "https://a/%G0",
		"https://a/caf%C3%A9", "https://a/cafe%CC%81", "https://a/café", "https://faß.de/",
		"https://a/#", "https://a/?#", "https://a/\x00", "https://a/\xff", "https://a/\xed\xa0\x80",
	} {
		urlLuaCompare(t, vm, s)
	}
	for _, scheme := range []string{"http", "https"} {
		for _, port := range []string{"0", "1", "80", "443", "65535", "65536", "08443", "00080", "000443", "+80", "-1", "", "1e2", " 1", "18446744073709551615"} {
			urlLuaCompare(t, vm, scheme+"://example.com:"+port+"/")
		}
	}
	for _, host := range []string{"127.0.0.1", "0.0.0.0", "255.255.255.255", "256.1.2.3", "1.2.3.04", "01.2.3.4", "1.2.3.4.5", "1.2.3"} {
		urlLuaCompare(t, vm, "https://"+host+"/")
	}
	for _, host := range []string{"::", "::1", "2001:db8::1", "2001:0db8:0000::1", "2001:DB8::1", "::ffff:192.0.2.1", "1:2:3:4:5:6:192.0.2.1", "1:2:3:4:5:6:7:8", "1:2:3:4:5:6:7::", "1:2:3:4:5:6:7:8::", "1:2:3:4:5:6:7", "1::2::3", ":1", "1:", ":::1", "::ffff:192.00.2.1", "::ffff:256.0.2.1", "::ffff:1.2.3.4:1", "127.0.0.1", "fe80::1%25eth0", "v1.a", "", "12345::"} {
		for _, suffix := range []string{"", ":8443", ":443", ":", "]"} {
			urlLuaCompare(t, vm, "https://["+host+"]"+suffix+"/")
		}
	}
	for _, n := range []int{1, 63, 64} {
		urlLuaCompare(t, vm, "https://"+strings.Repeat("a", n)+".com/")
	}
	for _, n := range []int{61, 62} {
		urlLuaCompare(t, vm, "https://"+strings.Repeat(strings.Repeat("a", 63)+".", 3)+strings.Repeat("b", n)+"/")
	}
	for _, n := range []int{2047, 2048, 2049} {
		const base = "https://example.com/"
		urlLuaCompare(t, vm, base+strings.Repeat("x", n-len(base)))
	}
	for b := 0; b < 256; b++ {
		for _, base := range []string{"https://example.com/", "https://example.com/?x="} {
			urlLuaCompare(t, vm, base+fmt.Sprintf("%%%02X", b))
			urlLuaCompare(t, vm, base+fmt.Sprintf("%%%02x", b))
			urlLuaCompare(t, vm, base+fmt.Sprintf("%%C2%%%02X", b))
			urlLuaCompare(t, vm, base+string([]byte{byte(b)}))
		}
	}
	rng := rand.New(rand.NewSource(47001))
	for i := 0; i < 128; i++ {
		var b [16]byte
		rng.Read(b[:])
		addr := netip.AddrFrom16(b)
		urlLuaCompare(t, vm, "https://["+addr.String()+"]/a?b=c")
	}
}

func TestURLLuaArgumentsAndIsolation(t *testing.T) {
	t.Parallel()
	vm := urlLuaNew(t, false)
	for _, deps := range [][2]lua.LValue{{lua.LNil, vm.data}, {vm.module, lua.LNil}, {vm.state.NewTable(), vm.data}, {vm.module, vm.state.NewTable()}} {
		if err := vm.state.CallByParam(lua.P{Fn: vm.factory, NRet: 1, Protect: true}, deps[0], deps[1]); err != nil {
			t.Fatal(err)
		}
		bad := vm.state.Get(-1).(*lua.LTable)
		vm.state.Pop(1)
		v, e := primitiveLuaInvoke(t, vm.primitiveLuaVM, bad.RawGetString("check_canonical"), lua.LString("https://example.com/"), lua.LNumber(1))
		if v != lua.LNil || e != lua.LString("INVALID_ARGUMENT") {
			t.Fatal(v, e)
		}
	}
	for _, version := range []lua.LValue{lua.LNil, lua.LString("1"), lua.LNumber(0), lua.LNumber(2), lua.LNumber(math.NaN()), lua.LNumber(math.Inf(1))} {
		v, e := vm.call(t, "check_canonical", lua.LString("https://example.com/"), version)
		if v != lua.LNil || e != lua.LString("INVALID_ARGUMENT") {
			t.Fatal(v, e)
		}
	}
	for _, s := range []lua.LValue{lua.LNil, lua.LTrue, lua.LNumber(1), vm.state.NewTable()} {
		v, e := vm.call(t, "check_canonical", s, lua.LNumber(1))
		if v != lua.LNil || e != lua.LString("INVALID_ARGUMENT") {
			t.Fatal(v, e)
		}
	}
	// Data and library tables are not mutated. The URL factory captures immutable
	// string bytes/functions rather than consulting caller-mutable table metadata.
	props := vm.data.RawGetString("props")
	vm.data.RawSetString("props", lua.LString("bad"))
	vm.data.RawSetString("get", lua.LNil)
	vm.module.RawSetString("parse_decimal", lua.LNil)
	vm.module.RawSetString("sha256", lua.LNil)
	urlLuaCompare(t, vm, "https://xn--bcher-kva.de/")
	urlLuaCompare(t, vm, "https://xn--bcher-kva.de:8443/")
	vm.data.RawSetString("props", props)
	for _, name := range []string{"redis", "io", "os", "package", "require", "loadfile", "loadstring", "dofile", "utf8"} {
		if vm.env.RawGetString(name) != lua.LNil {
			t.Fatalf("impure Lua environment: %s", name)
		}
	}
}

func rawALabel(t *testing.T, s string) string {
	t.Helper()
	a, err := idna.Punycode.ToASCII(s)
	if err != nil {
		t.Fatalf("raw punycode %q: %v", s, err)
	}
	return a
}

func TestURLLuaIDNAOracle(t *testing.T) {
	t.Parallel()
	vm := urlLuaNew(t, false)
	tests := []struct {
		s    string
		want bool
	}{
		{"faß", true}, {"bücher", true}, {"É", false}, {"1é", true}, {"a·b", true}, {"☃", true}, {"😀", true},
		{"क्\u200dष", true}, {"ب\u200cب", true}, {"ب\u200c1", true}, {"a\u200cb", false},
		{"\u0903a", false}, {"q" + strings.Repeat("\u0301", 31), false}, {"é--a", false}, {"éa--b", true},
		{"e\u0301", false}, {"aא", false}, {"אa", false}, {"א1١", false}, {"א1", true}, {"א١", true}, {"א\u05b0", true},
		{"א·", false}, {"\u0301a", false}, {"a\u00ada", false}, {"\ue000", false}, {"\ufdd0", false}, {"\u0378", false},
		{"\U0001fae8", true}, {"\U0001fae9", false}, {"ς", true}, {"σ", true},
	}
	var seeds []string
	for _, tc := range tests {
		a := rawALabel(t, tc.s)
		s := "https://" + a + ".example/"
		accepted := urlLuaCompare(t, vm, s)
		if accepted != tc.want {
			t.Fatalf("prior source-derived expectation disproved by Go for %q (%s): want %v got %v", tc.s, a, tc.want, accepted)
		}
		t.Logf("Go oracle: U-label=%q A-label=%s accepted=%v", tc.s, a, accepted)
		seeds = append(seeds, a)
	}
	for _, a := range []string{"xn--", "xn--a", "xn--abc-", "xn---abc", "xn--a-0hc", "xn--ab-j1t", "xn--99999999999999999999999999999999999999999999999999999999999", "xn--zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"} {
		urlLuaCompare(t, vm, "https://"+a+".com/")
	}
	urlLuaCompare(t, vm, "https://xn--4db.123.xn--1-bga/")
	for _, decoded := range []string{strings.Repeat("中", 40), strings.Repeat("\U00020000", 40), "q" + strings.Repeat("\u0301", 30)} {
		a := rawALabel(t, decoded)
		if !urlLuaCompare(t, vm, "https://"+a+".example/") {
			t.Fatalf("valid long decoded label rejected: %q", decoded)
		}
		urlLuaCompare(t, vm, "https://"+a+"."+a+"."+a+"."+a+"/")
	}
	// All length-1/2 punycode payloads, malformed tails and int32 overflow paths.
	alphabet := "abcdefghijklmnopqrstuvwxyz0123456789"
	for _, a := range alphabet {
		for _, b := range alphabet {
			urlLuaCompare(t, vm, "https://xn--"+string(a)+string(b)+".com/")
		}
	}
	rng := rand.New(rand.NewSource(151313))
	for i := 0; i < 1500; i++ {
		a := []byte(seeds[rng.Intn(len(seeds))])
		pos := 4 + rng.Intn(len(a)-3)
		switch rng.Intn(3) {
		case 0:
			a = append(a[:pos], append([]byte{alphabet[rng.Intn(len(alphabet))]}, a[pos:]...)...)
		case 1:
			if pos < len(a) {
				a = append(a[:pos], a[pos+1:]...)
			}
		case 2:
			if pos < len(a) {
				a[pos] = alphabet[rng.Intn(len(alphabet))]
			}
		}
		urlLuaCompare(t, vm, "https://"+string(a)+".example/a%FF?x=+")
	}
	// Supplementary planes, unassigned scalars, random valid Punycode and random
	// digit streams exercise the real decoder/encoder, not a handpicked whitelist.
	for i := 0; i < 3000; i++ {
		var u strings.Builder
		for n := 1 + rng.Intn(12); n > 0; n-- {
			r := rune(128 + rng.Intn(utf8.MaxRune-127))
			if r >= 0xd800 && r <= 0xdfff {
				r = 'é'
			}
			u.WriteRune(r)
		}
		urlLuaCompare(t, vm, "https://"+rawALabel(t, u.String())+".com/")
		var a strings.Builder
		a.WriteString("xn--")
		for n := 1 + rng.Intn(59); n > 0; n-- {
			a.WriteByte(alphabet[rng.Intn(len(alphabet))])
		}
		urlLuaCompare(t, vm, "https://"+a.String()+".com/")
	}
	// Structured ContextJ/bidi contexts, including transparent characters,
	// virama double transitions, default joining types and unresolved tails.
	contexts := []string{"a", "1", "-", "ب", "ا", "\u064e", "\u094d", "क", "\u0903", "\u200c", "\u200d", "א", "١"}
	for _, a := range contexts {
		for _, b := range contexts {
			for _, j := range []string{"\u200c", "\u200d"} {
				s := a + j + b
				urlLuaCompare(t, vm, "https://"+rawALabel(t, s)+".com/")
			}
		}
	}
}

func TestURLLuaNFCOracle(t *testing.T) {
	t.Parallel()
	vm := urlLuaNew(t, true)
	check := func(s string) {
		t.Helper()
		runes := []rune(s)
		a := vm.state.NewTable()
		for i, r := range runes {
			a.RawSetInt(i+1, lua.LNumber(r))
		}
		v, e := vm.call(t, "nfc", a)
		want := norm.NFC.IsNormalString(s)
		if v != lua.LBool(want) || e != lua.LNil {
			t.Fatalf("NFC %q: Lua=%v/%v Go=%v normalized=%q", s, v, e, want, norm.NFC.String(s))
		}
	}
	for _, s := range []string{"", "abc", "e\u0301", "é", "é\u0323", "ḋ\u0323", "q\u0301", "\u0301\u0323", "\U00020061\u0301", "\u1100\u1161\u11a8", "\u1100\u0301\u1161", "\u1100\u1161a\u0301"} {
		check(s)
	}
	for r := rune(0); r <= utf8.MaxRune; r++ {
		if r >= 0xd800 && r <= 0xdfff {
			continue
		}
		s := string(r)
		if norm.NFC.PropertiesString(s).Decomposition() != nil {
			check(s)
			check("q" + s)
			check(s + "\u0301")
			check(s + "\u0323")
			check(norm.NFD.String(s))
		}
		if r >= 0xac00 && r < 0xd7a4 {
			check(s)
			check(norm.NFD.String(s))
		}
	}
	for _, starter := range []string{"q", "é", "ḋ", "가", "각", "\u1100", "\u1161", "\u11a8", "א", "\U00020061"} {
		for _, mark := range []string{"\u0301", "\u0323", "\u034f", "\u094d", "\u200c", "\u0903"} {
			for _, n := range []int{0, 1, 2, 28, 29, 30, 31, 32, 50} {
				check(starter + strings.Repeat(mark, n))
			}
		}
	}
	rng := rand.New(rand.NewSource(3492))
	chars := []rune("aéḋqאبक가각\u0301\u0323\u0300\u034f\u094d\u0903\u1100\u1161\u11a8\u200c\u200d\U00020061")
	for i := 0; i < 4000; i++ {
		var s strings.Builder
		for n := rng.Intn(58) + 1; n > 0; n-- {
			s.WriteRune(chars[rng.Intn(len(chars))])
		}
		check(s.String())
	}
}

func TestURLLuaUnicodeGenerationAndExhaustiveProperties(t *testing.T) {
	t.Parallel()
	vm := urlLuaNew(t, false)
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	oracle := filepath.Join(tmp, "oracle.bin")
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", "-mod=readonly", "./internal/database/crawljobsv2/tools/generate-unicode", "-work", tmp, "-check", "-oracle", oracle)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("offline generator: %v\n%s", err, out)
	} else {
		t.Log(string(out))
	}
	raw, err := os.ReadFile(oracle)
	if err != nil {
		t.Fatal(err)
	}
	props := []byte(vm.data.RawGetString("props").(lua.LString))
	decomp := []byte(vm.data.RawGetString("decomp").(lua.LString))
	lookup := vm.data.RawGetString("get")
	pos := 0
	for cp := 0; cp <= utf8.MaxRune; cp++ {
		if pos+6 > len(raw) {
			t.Fatal("short property oracle")
		}
		head := raw[pos : pos+6]
		pos += 6
		v, e := primitiveLuaInvoke(t, vm.primitiveLuaVM, lookup, lua.LNumber(cp))
		if e != lua.LNil {
			t.Fatal(e)
		}
		offset := int(v.(lua.LNumber)) - 1
		if offset < 0 || offset+9 > len(props) || !bytes.Equal(props[offset:offset+5], head[:5]) || props[offset+8] != head[5] {
			t.Fatalf("property mismatch U+%04X", cp)
		}
		doff := int(props[offset+5])*65536 + int(props[offset+6])*256 + int(props[offset+7])
		for j := 0; j < int(head[5]); j++ {
			want := binary.BigEndian.Uint32(raw[pos : pos+4])
			pos += 4
			got := uint32(decomp[doff])*65536 + uint32(decomp[doff+1])*256 + uint32(decomp[doff+2])
			doff += 3
			if got != want {
				t.Fatalf("decomposition mismatch U+%04X", cp)
			}
		}
	}
	n := int(binary.BigEndian.Uint32(raw[pos : pos+4]))
	pos += 4
	if pos+n != len(raw) {
		t.Fatal("composition framing")
	}
	composition := vm.data.RawGetString("compose")
	for ; pos < len(raw); pos += 8 {
		key := binary.BigEndian.Uint32(raw[pos : pos+4])
		want := binary.BigEndian.Uint32(raw[pos+4 : pos+8])
		v, e := primitiveLuaInvoke(t, vm.primitiveLuaVM, composition, lua.LNumber(key>>16), lua.LNumber(key&65535))
		if v != lua.LNumber(want) || e != lua.LNil {
			t.Fatalf("composition mismatch %x", key)
		}
	}
	t.Log("actual Lua lookup agrees with pinned Go internals for all 1,114,112 code points and all composition entries")
}

// This test is independent of the candidate Lua implementation AND its data
// generator. Expectations come exclusively from existing Go URL/origin/digest,
// structural chunk validators and x/text NFC. Python consumes these expectations
// in a separate interpreter; neither implementation supplies its own oracle.
func TestPythonURLGoOracle(t *testing.T) {
	t.Parallel()
	if runtime.Version() != "go1.25.13" || unicode.Version != "15.0.0" || norm.Version != "15.0.0" || idna.UnicodeVersion != "15.0.0" || bidi.UnicodeVersion != "15.0.0" {
		t.Fatal("Python differential oracle requires Go 1.25.13 / Unicode 15.0.0")
	}
	var urls, normalizations, chunks []map[string]any
	seen := make(map[string]bool)
	addURL := func(value string) {
		if seen[value] {
			return
		}
		seen[value] = true
		identity, err := utils.CanonicalizeURLV1(value)
		valid := err == nil && identity.CanonicalURL == value
		item := map[string]any{"hex": hex.EncodeToString([]byte(value)), "valid": valid, "origin": nil}
		if origin, err := DeriveCanonicalOrigin(value); err == nil {
			item["origin"] = string(origin)
		}
		if valid {
			parsed, err := url.Parse(value)
			if err != nil {
				t.Fatal(err)
			}
			port := 80
			if parsed.Scheme == "https" {
				port = 443
			}
			if parsed.Port() != "" {
				port, err = strconv.Atoi(parsed.Port())
				if err != nil {
					t.Fatal(err)
				}
			}
			item["scheme"], item["host"], item["port"] = parsed.Scheme, parsed.Hostname(), port
			item["explicit_port"], item["is_ip"] = parsed.Port() != "", net.ParseIP(parsed.Hostname()) != nil
			item["url_id"] = identity.URLID
			digest, err := DeriveTargetDigest(RequestTarget{URLID: JobID(identity.URLID), CanonicalURL: value})
			if err != nil {
				t.Fatal(err)
			}
			item["target_digest"] = string(digest)
		}
		urls = append(urls, item)
	}
	addNFC := func(value string) {
		normalizations = append(normalizations, map[string]any{"text": value, "normal": norm.NFC.IsNormalString(value)})
	}
	var shared struct {
		Valid []struct {
			Input     string `json:"input"`
			Canonical string `json:"canonical_url"`
		}
		Invalid []struct {
			Input string `json:"input"`
		}
	}
	data, err := os.ReadFile("../../utils/testdata/url-canonicalization-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &shared); err != nil {
		t.Fatal(err)
	}
	for _, v := range shared.Valid {
		addURL(v.Input)
		addURL(v.Canonical)
	}
	for _, v := range shared.Invalid {
		addURL(v.Input)
	}
	for _, value := range []string{
		"", "https://", "https:///x", "https://a", "https://a?x=/", "https://a/?", "https://a/??", "https://a/@user:pass?q=@x",
		"https://a/a//../b/?x=1&x=&x=2;+", "https://a/%FF", "https://a/%80", "https://a/%ED%A0%80", "https://a/%C0%AF", "https://a/%2500", "https://a/%5C",
		"https://a/%C2x%85", "https://a/%C2%2585", "https://a/%", "https://a/%0", "https://a/%g0", "https://a/#", "https://a/?#", "https://@a/",
		"https://a/\xff", "https://a/\xed\xa0\x80", "https://a/\xc0\x80", "https://a/\u0085", "https://a/\u2028", "https://a/\u00a0", "https://a/café",
		"https://a/caf%C3%A9", "https://a/cafe%CC%81", "https://a/%61", "https://a/a", "https://a/%7E", "https://a/~",
	} {
		addURL(value)
	}
	for b := 0; b < 256; b++ {
		for _, prefix := range []string{"https://a/", "https://a/?x="} {
			addURL(prefix + fmt.Sprintf("%%%02X", b))
			addURL(prefix + fmt.Sprintf("%%%02x", b))
			addURL(prefix + fmt.Sprintf("%%C2%%%02X", b))
			addURL(prefix + string([]byte{byte(b)}))
		}
	}
	for _, host := range []string{"localhost", "service.local", "service.onion", "service.test", "service.example", "127.1", "2130706433", "ab--cd.example", "0x7f.0.0.1", "127.0.0.1", "0.0.0.0", "255.255.255.255", "256.0.0.1", "1.2.3.04", "00.1.2.3", "1.2.3.4.5", "a.", "a..b", "a_b.com", "-a.com", "a-.com", "A.com", "%65xample.com", "½.com"} {
		addURL("https://" + host + "/")
	}
	for _, scheme := range []string{"http", "https"} {
		for _, port := range []string{"", "0", "1", "80", "443", "65535", "65536", "08443", "00080", "000443", "-1", "+1", "1e2", "1.5", "½", "١", "18446744073709551615"} {
			addURL(scheme + "://example.com:" + port + "/")
		}
	}
	for _, host := range []string{"::", "::1", "2001:db8::1", "2001:0db8:0000::1", "2001:DB8::1", "::ffff:192.0.2.1", "1:2:3:4:5:6:192.0.2.1", "1:2:3:4:5:6:7:8", "1:2:3:4:5:6:7::", "1:2:3:4:5:6:7:8::", "1::2::3", ":1", "1:", ":::1", "::ffff:192.00.2.1", "::ffff:256.0.2.1", "::ffff:1.2.3.4:1", "127.0.0.1", "fe80::1%25eth0", "v1.a", "", "12345::"} {
		for _, suffix := range []string{"", ":8443", ":443", ":", "]"} {
			addURL("https://[" + host + "]" + suffix + "/")
		}
	}
	for _, n := range []int{1, 63, 64} {
		addURL("https://" + strings.Repeat("a", n) + ".com/")
	}
	for _, n := range []int{61, 62} {
		addURL("https://" + strings.Repeat(strings.Repeat("a", 63)+".", 3) + strings.Repeat("b", n) + "/")
	}
	for _, n := range []int{2047, 2048, 2049} {
		const base = "https://example.com/"
		addURL(base + strings.Repeat("x", n-len(base)))
	}
	var alabels []string
	for _, decoded := range []string{"faß", "bücher", "É", "1é", "a·b", "☃", "😀", "क्\u200dष", "ب\u200cب", "ب\u200c1", "a\u200cb", "\u0903a", "é--a", "éa--b", "ab--é", "e\u0301", "aא", "אa", "א1١", "א1", "א١", "א\u05b0", "א·", "¼", "²", "\u0378", "\ue000", "\ufdd0", "\U0001fae8", "\U0001fae9", "ς", "\U00020061\u0301", strings.Repeat("中", 40), strings.Repeat("\U00020000", 40), "q" + strings.Repeat("\u0301", 30), "q" + strings.Repeat("\u0301", 31)} {
		a := rawALabel(t, decoded)
		alabels = append(alabels, a)
		addURL("https://" + a + ".example/")
		addNFC(decoded)
	}
	addURL("https://xn--4db.123.xn--1-bga/")
	for _, a := range []string{"xn--", "xn--a", "xn--abc-", "xn---abc", "xn--a-0hc", "xn--ab-j1t", "xn--" + strings.Repeat("z", 59), "xn--" + strings.Repeat("9", 59)} {
		addURL("https://" + a + ".com/")
	}
	for _, a := range []string{"a", "1", "-", "ب", "ا", "\u064e", "\u094d", "क", "\u0903", "\u200c", "\u200d", "א", "١"} {
		for _, b := range []string{"a", "1", "-", "ب", "ا", "\u064e", "\u094d", "क", "\u0903", "\u200c", "\u200d", "א", "١"} {
			for _, j := range []string{"\u200c", "\u200d"} {
				addURL("https://" + rawALabel(t, a+j+b) + ".com/")
			}
		}
	}
	rng := rand.New(rand.NewSource(15002513))
	alphabet := "abcdefghijklmnopqrstuvwxyz0123456789"
	for i := 0; i < 3000; i++ {
		var decoded, payload strings.Builder
		for n := 1 + rng.Intn(12); n > 0; n-- {
			r := rune(128 + rng.Intn(utf8.MaxRune-127))
			if r >= 0xd800 && r <= 0xdfff {
				r = 'é'
			}
			decoded.WriteRune(r)
		}
		addURL("https://" + rawALabel(t, decoded.String()) + ".com/")
		for n := 1 + rng.Intn(59); n > 0; n-- {
			payload.WriteByte(alphabet[rng.Intn(len(alphabet))])
		}
		addURL("https://xn--" + payload.String() + ".com/")
		a := []byte(alabels[rng.Intn(len(alabels))])
		pos := 4 + rng.Intn(len(a)-4)
		a[pos] = alphabet[rng.Intn(len(alphabet))]
		addURL("https://" + string(a) + ".com/")
	}
	for i := 0; i < 128; i++ {
		var b [16]byte
		rng.Read(b[:])
		addURL("https://[" + netip.AddrFrom16(b).String() + "]/a?b=c")
	}
	for r := rune(0); r <= utf8.MaxRune; r++ {
		if r >= 0xd800 && r <= 0xdfff {
			continue
		}
		s := string(r)
		if norm.NFC.PropertiesString(s).Decomposition() != nil {
			addNFC(s)
			addNFC("q" + s)
			addNFC(s + "\u0301")
			addNFC(s + "\u0323")
			addNFC(norm.NFD.String(s))
		}
		if r >= 0xac00 && r < 0xd7a4 {
			addNFC(s)
			addNFC(norm.NFD.String(s))
		}
	}
	for _, starter := range []string{"q", "é", "ḋ", "가", "각", "\u1100", "\u1161", "\u11a8", "א", "\U00020061"} {
		for _, mark := range []string{"\u0301", "\u0323", "\u034f", "\u094d", "\u200c", "\u0903"} {
			for _, n := range []int{0, 1, 2, 28, 29, 30, 31, 32, 50} {
				addNFC(starter + strings.Repeat(mark, n))
			}
		}
	}
	chars := []rune("aéḋqאبक가각\u0301\u0323\u0300\u034f\u094d\u0903\u1100\u1161\u11a8\u200c\u200d\U00020061")
	for i := 0; i < 4000; i++ {
		var s strings.Builder
		for n := 1 + rng.Intn(58); n > 0; n-- {
			s.WriteRune(chars[rng.Intn(len(chars))])
		}
		addNFC(s.String())
	}
	// Prove the structural-vs-policy distinction against the real Go codecs;
	// this is not authorization to build an IP-bearing request or source job.
	for _, value := range []string{"http://127.0.0.1/", "https://[2001:0db8:0000::1]/", "https://example.com:8443/"} {
		publication := strings.Repeat("1", 64)
		encodedURL := base64.RawURLEncoding.EncodeToString([]byte(value))
		imageKeys, err := json.Marshal([]string{"image_data:" + publication + ":" + encodedURL + ":" + encodedURL})
		if err != nil {
			t.Fatal(err)
		}
		records := map[ChunkKind]Record{
			ChunkPageFields:    {textField("normalized_url", value), textField("content_type", "text/html"), textField("status_code", "200"), textField("last_crawled", formatRedisLastCrawled(RedisMilliseconds(1788266096789))), textField("rendered", "false"), textField("render_policy_rule", ""), textField("render_policy_sha256", ""), textField("publication_id", publication)},
			ChunkImageManifest: {textField("contract_version", "1"), textField("publication_id", publication), textField("normalized_url", value), textField("image_count", "1"), textField("image_keys", string(imageKeys))},
			ChunkOutlinks:      {textField("target_url", value)},
			ChunkImages:        {textField("normalized_source_url", value), textField("alt", "")},
			ChunkAliases:       {textField("url_id", utils.URLIDV1(value)), textField("canonical_url", value), textField("depth", "0")},
			ChunkDiscoveries:   {textField("job_id", utils.URLIDV1(value)), textField("canonical_url", value), textField("depth", "0"), textField("score_text", "0"), textField("group_id", "group-a"), textField("rate_scope_id", strings.Repeat("1", 32)), textField("group_scope_id", strings.Repeat("1", 64)), textField("initial_origin_scope_id", strings.Repeat("1", 64)), textField("policy_decision_sha256", strings.Repeat("1", 64))},
		}
		validators := map[ChunkKind]func(StageChunk) error{ChunkOutlinks: validateOutlinksChunk, ChunkImages: validateImagesChunk, ChunkAliases: validateAliasesChunk, ChunkDiscoveries: validateDiscoveriesChunk, ChunkPageFields: validatePageFieldsChunk, ChunkImageManifest: validateImageManifestChunk}
		for kind, record := range records {
			if err := validators[kind](StageChunk{kind: kind, records: []Record{record}}); err != nil {
				t.Fatal(err)
			}
			var fields [][2]string
			for _, field := range record {
				fields = append(fields, [2]string{field.Name, string(field.Value)})
			}
			chunks = append(chunks, map[string]any{"kind": string(kind), "fields": fields})
		}
	}
	encoded, err := json.Marshal(map[string]any{"urls": urls, "nfc": normalizations, "chunks": chunks})
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../../../..")
	if err != nil {
		t.Fatal(err)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python differential conformance requires python3; cannot silently skip")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-I", "-B", filepath.Join(root, "scripts/tests/test_crawl_jobs_v2_url.py"), "--go-oracle")
	cmd.Dir = t.TempDir()
	cmd.Stdin = bytes.NewReader(encoded)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Go/Python differential conformance: %v\n%s", err, output)
	}
	t.Logf("independent Go/Python URL/NFC/codec comparison: %s", output)
}
