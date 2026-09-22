package crawljobsv2

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/bits"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	lua "github.com/yuin/gopher-lua"
)

// Independent of the BOOT harness and any service, Redis emulator, or bundle.
const (
	primitiveLuaMaxBytes   = 16 * 1024 * 1024
	primitiveLuaMaxFields  = 128
	primitiveLuaMaxRecords = 10000
)

type primitiveLuaVM struct {
	state  *lua.LState
	module *lua.LTable
	env    *lua.LTable
}

// In-memory Lua conformance uses Go's test lifetime and budget, not a Redis
// script-latency limit. Preserve test cancellation even with -timeout=0.
func luaTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx := t.Context()
	var cancel context.CancelFunc
	if deadline, ok := t.Deadline(); ok {
		ctx, cancel = context.WithDeadline(ctx, deadline)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	t.Cleanup(cancel)
	return ctx
}

func primitiveLuaRead(t *testing.T, relative string) []byte {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate primitive test source")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), relative))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// LuaBitOp's double-number conversion uses this bias, not Go's truncating
// float-to-int conversion. It rounds ties to even and retains the low 32 bits.
// Inputs exercised here (including all SHA sums) are inside BitOp's +/-2^51
// defined input range. All exported results are SIGNED Lua numbers.
func primitiveLuaBitWord(l *lua.LState, index int) uint32 {
	return uint32(math.Float64bits(float64(l.CheckNumber(index)) + 6755399441055744.0))
}

func primitiveLuaBitOp(l *lua.LState) *lua.LTable {
	table := l.NewTable()
	for _, name := range []string{"tobit", "band", "bor", "bxor", "bnot", "lshift", "rshift", "arshift", "rol", "ror", "bswap"} {
		table.RawSetString(name, l.NewFunction(func(l *lua.LState) int {
			word := primitiveLuaBitWord(l, 1)
			switch name {
			case "band", "bor", "bxor":
				for i := 2; i <= l.GetTop(); i++ {
					other := primitiveLuaBitWord(l, i)
					switch name {
					case "band":
						word &= other
					case "bor":
						word |= other
					case "bxor":
						word ^= other
					}
				}
			case "bnot":
				word = ^word
			case "lshift", "rshift", "arshift", "rol", "ror":
				shift := primitiveLuaBitWord(l, 2) & 31
				switch name {
				case "lshift":
					word <<= shift
				case "rshift":
					word >>= shift
				case "arshift":
					word = uint32(int32(word) >> shift)
				case "rol":
					word = bits.RotateLeft32(word, int(shift))
				case "ror":
					word = bits.RotateLeft32(word, -int(shift))
				}
			case "bswap":
				word = bits.ReverseBytes32(word)
			}
			l.Push(lua.LNumber(int32(word)))
			return 1
		}))
	}
	return table
}

func primitiveLuaNew(t *testing.T, withBit bool) *primitiveLuaVM {
	t.Helper()
	// Gopher-Lua's table.concat pushes each part AND separator on its registry
	// (unlike native Lua 5.1). SECTION has 2*max_records+3 parts; reserve that
	// computed worst case plus 1024 registers for the bounded call chain.
	l := lua.NewState(lua.Options{SkipOpenLibs: true, RegistrySize: 4*primitiveLuaMaxRecords + 6 + 1024})
	t.Cleanup(l.Close)
	l.SetContext(luaTestContext(t))
	lua.OpenBase(l)
	lua.OpenMath(l)
	lua.OpenString(l)
	lua.OpenTable(l)
	l.SetTop(0)
	// Only these pure functions are visible to the actual loaded Lua chunk.
	// No IO, sockets, OS, Redis, package, loaders, or test-side hash callbacks.
	env := l.NewTable()
	for _, name := range []string{"type", "next", "rawget", "getmetatable"} {
		env.RawSetString(name, l.GetGlobal(name))
	}
	for name, fields := range map[string][]string{
		"math": {"floor"}, "string": {"byte", "char", "sub", "rep", "format"}, "table": {"concat"},
	} {
		table := l.NewTable()
		for _, field := range fields {
			table.RawSetString(field, l.GetField(l.GetGlobal(name), field))
		}
		env.RawSetString(name, table)
	}
	if withBit {
		env.RawSetString("bit", primitiveLuaBitOp(l))
	}
	// Snapshot even the library members, to detect environment mutation rather
	// than merely checking that no new top-level globals were written.
	snapshot := map[*lua.LTable]map[lua.LValue]lua.LValue{env: {}}
	env.ForEach(func(k, v lua.LValue) {
		snapshot[env][k] = v
		if child, ok := v.(*lua.LTable); ok {
			snapshot[child] = make(map[lua.LValue]lua.LValue)
			child.ForEach(func(k, v lua.LValue) { snapshot[child][k] = v })
		}
	})
	t.Cleanup(func() {
		for table, before := range snapshot {
			after := 0
			table.ForEach(func(k, v lua.LValue) {
				after++
				if before[k] != v {
					t.Errorf("Lua mutated environment member %s", k)
				}
			})
			if after != len(before) {
				t.Error("Lua changed environment member count")
			}
		}
	})
	fn, err := l.Load(bytes.NewReader(primitiveLuaRead(t, "lua_src/primitives.lua")), "primitives.lua")
	if err != nil {
		t.Fatal(err)
	}
	l.SetFEnv(fn, env)
	if err := l.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}); err != nil {
		t.Fatal(err)
	}
	module, ok := l.Get(-1).(*lua.LTable)
	if !ok {
		t.Fatal("primitive source did not return a module")
	}
	l.Pop(1)
	return &primitiveLuaVM{state: l, module: module, env: env}
}

func primitiveLuaInvoke(t *testing.T, vm *primitiveLuaVM, fn lua.LValue, args ...lua.LValue) (lua.LValue, lua.LValue) {
	t.Helper()
	if err := vm.state.CallByParam(lua.P{Fn: fn, NRet: 2, Protect: true}, args...); err != nil {
		t.Fatalf("Lua raised instead of returning a stable rejection: %v", err)
	}
	value, failure := vm.state.Get(-2), vm.state.Get(-1)
	vm.state.Pop(2)
	return value, failure
}

func primitiveLuaOK(t *testing.T, vm *primitiveLuaVM, name string, args ...lua.LValue) lua.LValue {
	t.Helper()
	value, err := primitiveLuaInvoke(t, vm, vm.module.RawGetString(name), args...)
	if value == lua.LNil || err != lua.LNil {
		t.Fatalf("%s returned %s, %s", name, value, err)
	}
	return value
}

func primitiveLuaError(t *testing.T, vm *primitiveLuaVM, name, want string, args ...lua.LValue) {
	t.Helper()
	value, err := primitiveLuaInvoke(t, vm, vm.module.RawGetString(name), args...)
	if value != lua.LNil || err != lua.LString(want) {
		t.Fatalf("%s returned %s, %s; want nil, %s", name, value, err, want)
	}
}

func primitiveLuaString(t *testing.T, value lua.LValue) string {
	t.Helper()
	s, ok := value.(lua.LString)
	if !ok {
		t.Fatalf("expected exact string, got %s", value.Type())
	}
	return string(s)
}

func primitiveLuaArray(vm *primitiveLuaVM, values ...lua.LValue) *lua.LTable {
	table := vm.state.NewTable()
	for i, value := range values {
		table.RawSetInt(i+1, value)
	}
	return table
}

func primitiveLuaRecord(vm *primitiveLuaVM, record Record) (*lua.LTable, *lua.LTable) {
	fields, names := vm.state.NewTable(), vm.state.NewTable()
	for i, field := range record {
		fields.RawSetInt(i+1, primitiveLuaArray(vm, lua.LString(field.Name), lua.LString(field.Value)))
		names.RawSetInt(i+1, lua.LString(field.Name))
	}
	return fields, names
}

func primitiveLuaEncoded(t *testing.T, record Record) []byte {
	t.Helper()
	encoded, err := EncodeRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestPrimitiveLuaSource(t *testing.T) {
	t.Parallel()
	vm := primitiveLuaNew(t, true)
	source := primitiveLuaRead(t, "lua_src/primitives.lua")
	t.Logf("core source files=1 lines=%d bytes=%d sha256=%x (informational, not an approval seal)",
		bytes.Count(source, []byte{'\n'}), len(source), sha256.Sum256(source))
	functions := strings.Fields("parse_decimal format_decimal safe_add time_ms validate_text u64 decode_u64 frame decode_frame record section decode_record sha256")
	for _, name := range functions {
		if vm.module.RawGetString(name).Type() != lua.LTFunction {
			t.Fatalf("missing API %s", name)
		}
	}
	count := 0
	vm.module.ForEach(func(_, _ lua.LValue) { count++ })
	if count != len(functions)+1 {
		t.Fatalf("unexpected module API count %d", count)
	}
	limits := vm.module.RawGetString("limits").(*lua.LTable)
	for key, value := range map[string]uint64{
		"max_integer": MaxExactInteger, "max_bytes": primitiveLuaMaxBytes,
		"max_fields": primitiveLuaMaxFields, "max_records": primitiveLuaMaxRecords,
	} {
		if limits.RawGetString(key) != lua.LNumber(value) {
			t.Fatalf("limit %s mismatch", key)
		}
	}
	// Exported metadata is not mutable authority for the actual bounds.
	limits.RawSetString("max_integer", lua.LNumber(math.MaxFloat64))
	primitiveLuaError(t, vm, "format_decimal", "INVALID_NUMBER", lua.LNumber(MaxExactInteger+1))
	withoutBit := primitiveLuaNew(t, false)
	primitiveLuaError(t, withoutBit, "sha256", "BIT_UNAVAILABLE", lua.LString("abc"))
	if primitiveLuaOK(t, withoutBit, "parse_decimal", lua.LString("42")) != lua.LNumber(42) {
		t.Fatal("non-bit primitives require BitOp")
	}
}

func TestPrimitiveLuaBitOp(t *testing.T) {
	t.Parallel()
	vm := primitiveLuaNew(t, true)
	for _, tc := range []struct {
		name string
		args []float64
		want int32
	}{
		{"tobit", []float64{4294967295}, -1}, {"tobit", []float64{4294967296}, 0},
		{"tobit", []float64{-4294967297}, -1}, {"tobit", []float64{0.5}, 0},
		{"tobit", []float64{1.5}, 2}, {"tobit", []float64{2.5}, 2}, {"tobit", []float64{-1.5}, -2},
		{"band", []float64{0xffffffff, 0x800000ff, 0xffffff0f}, -2147483633},
		{"bor", []float64{0x80000000, 1, 2}, -2147483645},
		{"bxor", []float64{0xffffffff, 0x0f0f0f0f, 0xf0f0f0f0}, 0},
		{"bnot", []float64{0}, -1}, {"lshift", []float64{1, 31}, math.MinInt32},
		{"lshift", []float64{1, 32}, 1}, {"lshift", []float64{1, -1}, math.MinInt32},
		{"rshift", []float64{-1, 1}, math.MaxInt32}, {"rshift", []float64{-1, 32}, -1},
		{"arshift", []float64{0x80000000, 1}, -1073741824},
		{"ror", []float64{1, 1}, math.MinInt32}, {"ror", []float64{0x80000000, 31}, 1},
		{"rol", []float64{0x80000000, 1}, 1}, {"bswap", []float64{0x12345678}, 0x78563412},
	} {
		args := make([]lua.LValue, len(tc.args))
		for i, n := range tc.args {
			args[i] = lua.LNumber(n)
		}
		got, err := primitiveLuaInvoke(t, vm, vm.state.GetField(vm.env.RawGetString("bit"), tc.name), args...)
		if got != lua.LNumber(tc.want) || err != lua.LNil {
			t.Fatalf("bit.%s(%v) = %s, %s; want signed %d", tc.name, tc.args, got, err, tc.want)
		}
	}
}

func TestPrimitiveLuaSHA256(t *testing.T) {
	t.Parallel()
	vm := primitiveLuaNew(t, true)
	for _, tc := range []struct{ input, known string }{
		{"", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"abc", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
		{"abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq", "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1"},
		{strings.Repeat("a", 1000000), "cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0"},
	} {
		want := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.input)))
		if want != tc.known || primitiveLuaOK(t, vm, "sha256", lua.LString(tc.input)) != lua.LString(want) {
			t.Fatalf("SHA-256 standard vector length %d mismatch", len(tc.input))
		}
	}
	rng := rand.New(rand.NewSource(0x51256))
	for _, size := range []int{0, 1, 55, 56, 63, 64, 65, 119, 120, 127, 128, 129, 256, 4095, 4096, 4097, 65535, 65536, 65537} {
		input := make([]byte, size)
		if _, err := rng.Read(input); err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("%x", sha256.Sum256(input))
		if got := primitiveLuaOK(t, vm, "sha256", lua.LString(input)); got != lua.LString(want) {
			t.Fatalf("SHA-256 binary length %d = %s, want %s", size, got, want)
		}
	}
	primitiveLuaError(t, vm, "sha256", "LIMIT_EXCEEDED", lua.LString(strings.Repeat("x", primitiveLuaMaxBytes+1)))
	primitiveLuaError(t, vm, "sha256", "INVALID_ARGUMENT", lua.LNumber(123))
	if primitiveLuaOK(t, vm, "sha256", lua.LString("abc")) != lua.LString(fmt.Sprintf("%x", sha256.Sum256([]byte("abc")))) {
		t.Fatal("hash state leaked across calls")
	}
}

func TestPrimitiveLuaExactNumbers(t *testing.T) {
	t.Parallel()
	vm := primitiveLuaNew(t, true)
	numbers := []uint64{0, 1, 9, 10, 99, 100, 255, 256, 4294967295, 4294967296, 1 << 52, MaxExactInteger - 2, MaxExactInteger - 1, MaxExactInteger}
	for n := MaxExactInteger - 512; n <= MaxExactInteger; n++ {
		numbers = append(numbers, n)
	}
	rng := rand.New(rand.NewSource(53))
	for range 300 {
		numbers = append(numbers, rng.Uint64()&MaxExactInteger)
	}
	for _, n := range numbers {
		decimal, err := CanonicalUnsignedDecimal(n)
		if err != nil {
			t.Fatal(err)
		}
		if primitiveLuaOK(t, vm, "parse_decimal", lua.LString(decimal)) != lua.LNumber(n) ||
			primitiveLuaOK(t, vm, "format_decimal", lua.LNumber(n)) != lua.LString(decimal) {
			t.Fatalf("canonical decimal round trip failed for %d", n)
		}
		encoded := primitiveLuaOK(t, vm, "u64", lua.LNumber(n))
		if encoded != lua.LString(U64(n)) || primitiveLuaOK(t, vm, "decode_u64", encoded) != lua.LNumber(n) {
			t.Fatalf("U64 round trip failed for %d", n)
		}
		if primitiveLuaOK(t, vm, "safe_add", lua.LNumber(n), lua.LNumber(MaxExactInteger-n)) != lua.LNumber(MaxExactInteger) {
			t.Fatalf("exact addition failed for %d", n)
		}
		primitiveLuaError(t, vm, "safe_add", "INVALID_NUMBER", lua.LNumber(n+1), lua.LNumber(MaxExactInteger-n))
	}
	for _, text := range []string{"", "00", "01", "-0", "-1", "+1", " 1", "1 ", "1\n", "1\x00", "1.0", "1e0", "0x10", "NaN", "inf", "١", "9007199254740992", "9007199254740993", "18446744073709551615", "9999999999999999", strings.Repeat("1", 100)} {
		if _, err := ParseUnsignedDecimal(text); err == nil {
			t.Fatalf("Go oracle unexpectedly accepted %q", text)
		}
		primitiveLuaError(t, vm, "parse_decimal", "INVALID_NUMBER", lua.LString(text))
	}
	for _, invalid := range []lua.LValue{lua.LNil, lua.LTrue, lua.LString("1"), vm.state.NewTable(), lua.LNumber(-1), lua.LNumber(0.5), lua.LNumber(math.NaN()), lua.LNumber(math.Inf(1)), lua.LNumber(math.Inf(-1)), lua.LNumber(MaxExactInteger + 1)} {
		primitiveLuaError(t, vm, "format_decimal", "INVALID_NUMBER", invalid)
		primitiveLuaError(t, vm, "u64", "INVALID_NUMBER", invalid)
		primitiveLuaError(t, vm, "safe_add", "INVALID_NUMBER", invalid, lua.LNumber(0))
		primitiveLuaError(t, vm, "safe_add", "INVALID_NUMBER", lua.LNumber(0), invalid)
	}
	primitiveLuaError(t, vm, "parse_decimal", "INVALID_NUMBER", lua.LNumber(1))
	for _, n := range []uint64{MaxExactInteger + 1, MaxExactInteger + 2, math.MaxUint64} {
		primitiveLuaError(t, vm, "decode_u64", "INVALID_NUMBER", lua.LString(U64(n)))
	}
}

func TestPrimitiveLuaTime(t *testing.T) {
	t.Parallel()
	vm := primitiveLuaNew(t, true)
	for _, pair := range [][2]uint64{{0, 0}, {0, 999}, {0, 1000}, {1, 999999}, {1700000000, 123456}, {9007199254740, 990999}, {9007199254740, 991999}} {
		reply := primitiveLuaArray(vm, lua.LString(strconv.FormatUint(pair[0], 10)), lua.LString(strconv.FormatUint(pair[1], 10)))
		want := pair[0]*1000 + pair[1]/1000
		if got := primitiveLuaOK(t, vm, "time_ms", reply); got != lua.LNumber(want) {
			t.Fatalf("TIME %v = %s, want %d", pair, got, want)
		}
	}
	for _, pair := range [][2]string{{"00", "0"}, {"1", "000001"}, {"1", "1000000"}, {"1", "-1"}, {"1e0", "0"}, {"1", "1.0"}, {"1 ", "0"}, {"9007199254740", "992000"}, {"9007199254741", "0"}, {"9007199254740991", "0"}, {"9007199254740992", "0"}} {
		primitiveLuaError(t, vm, "time_ms", "INVALID_TIME", primitiveLuaArray(vm, lua.LString(pair[0]), lua.LString(pair[1])))
	}
	for _, value := range []lua.LValue{lua.LNil, lua.LString("1,2"), primitiveLuaArray(vm), primitiveLuaArray(vm, lua.LString("1")), primitiveLuaArray(vm, lua.LString("1"), lua.LString("2"), lua.LString("3")), primitiveLuaArray(vm, lua.LNumber(1), lua.LString("2")), primitiveLuaArray(vm, lua.LString("1"), lua.LNumber(2))} {
		primitiveLuaError(t, vm, "time_ms", "INVALID_TIME", value)
	}
}

// Exactly four bare-framing edge families; these do not apply text validation.
func TestPrimitiveLuaBareFramingEdges(t *testing.T) {
	t.Parallel()
	vm := primitiveLuaNew(t, true)
	t.Run("zero_lengths", func(t *testing.T) {
		if primitiveLuaOK(t, vm, "frame", lua.LString("")) != lua.LString(F(nil)) ||
			primitiveLuaOK(t, vm, "decode_frame", lua.LString(F(nil))) != lua.LString("") {
			t.Fatal("empty frame mismatch")
		}
		empty := primitiveLuaArray(vm)
		if primitiveLuaOK(t, vm, "record", empty) != lua.LString(U64(0)) {
			t.Fatal("empty record mismatch")
		}
		primitiveLuaOK(t, vm, "decode_record", lua.LString(U64(0)), empty)
	})
	t.Run("nonminimal_width", func(t *testing.T) {
		for _, s := range []string{"\x00" + string(U64(0)), "\x01A", "00000001A"} {
			primitiveLuaError(t, vm, "decode_u64", "INVALID_ENCODING", lua.LString(s))
		}
		primitiveLuaError(t, vm, "decode_frame", "INVALID_ENCODING", lua.LString("\x00"+string(F(nil))))
	})
	t.Run("truncated", func(t *testing.T) {
		encoded := F([]byte("abc"))
		for i := 0; i < len(encoded); i++ {
			primitiveLuaError(t, vm, "decode_frame", "INVALID_ENCODING", lua.LString(encoded[:i]))
		}
	})
	t.Run("malformed", func(t *testing.T) {
		primitiveLuaError(t, vm, "decode_frame", "INVALID_NUMBER", lua.LString(U64(math.MaxUint64)))
		primitiveLuaError(t, vm, "decode_frame", "INVALID_ENCODING", lua.LString(U64(MaxExactInteger)))
		primitiveLuaError(t, vm, "decode_frame", "INVALID_ENCODING", lua.LString(append(F([]byte("x")), 0)))
		primitiveLuaError(t, vm, "decode_frame", "INVALID_ARGUMENT", lua.LNumber(0))
	})
}

func TestPrimitiveLuaFramingConformance(t *testing.T) {
	t.Parallel()
	vm := primitiveLuaNew(t, true)
	allBytes := make([]byte, 256)
	for i := range allBytes {
		allBytes[i] = byte(i)
	}
	for _, value := range [][]byte{nil, []byte("A"), []byte("\x00\xff\x80\r\n"), []byte("é雪😀"), allBytes, bytes.Repeat([]byte{'a'}, 128)} {
		got := primitiveLuaOK(t, vm, "frame", lua.LString(value))
		if got != lua.LString(F(value)) || primitiveLuaOK(t, vm, "decode_frame", got) != lua.LString(value) {
			t.Fatal("binary F mismatch")
		}
	}
	rng := rand.New(rand.NewSource(64))
	for n := 0; n < 32; n++ {
		record := make(Record, n)
		for i := range record {
			value := make([]byte, rng.Intn(257))
			if _, err := rng.Read(value); err != nil {
				t.Fatal(err)
			}
			record[i] = Field{Name: fmt.Sprintf("field %02d~", n-i), Value: value}
		}
		fields, names := primitiveLuaRecord(vm, record)
		got := primitiveLuaOK(t, vm, "record", fields)
		encoded := primitiveLuaEncoded(t, record)
		if got != lua.LString(encoded) {
			t.Fatalf("RECORD %d mismatch", n)
		}
		decoded := primitiveLuaOK(t, vm, "decode_record", got, names, lua.LNumber(len(encoded)), lua.LFalse)
		if primitiveLuaOK(t, vm, "record", decoded) != got {
			t.Fatal("binary RECORD round trip lost order or bytes")
		}
		section, err := EncodeSection("section ~", []Record{record, {}, record})
		if err != nil {
			t.Fatal(err)
		}
		if primitiveLuaOK(t, vm, "section", lua.LString("section ~"), primitiveLuaArray(vm, fields, primitiveLuaArray(vm), fields)) != lua.LString(section) {
			t.Fatal("SECTION nesting mismatch")
		}
	}
}

func TestPrimitiveLuaSharedJSON(t *testing.T) {
	t.Parallel()
	vm := primitiveLuaNew(t, true)
	// Read the shared JSON directly; no existing fixture or BOOT test helpers.
	type primitiveLuaVector struct {
		Name     string            `json:"name"`
		Kind     string            `json:"kind"`
		Input    map[string]string `json:"input"`
		Expected map[string]string `json:"expected"`
	}
	var fixture struct {
		Expected struct {
			Framing map[string]string `json:"framing"`
		} `json:"expected"`
		Cases []primitiveLuaVector
	}
	// Non-primitive case inputs contain objects and arrays; decode those only
	// after selecting a primitive kind rather than coercing JSON into strings.
	var document struct {
		Expected json.RawMessage
		Cases    []json.RawMessage
	}
	if err := json.Unmarshal(primitiveLuaRead(t, "../../../../../contracts/crawl-jobs-v2/digest-vectors.json"), &document); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(document.Expected, &fixture.Expected); err != nil {
		t.Fatal(err)
	}
	for _, raw := range document.Cases {
		var kind struct{ Kind string }
		if err := json.Unmarshal(raw, &kind); err != nil {
			t.Fatal(err)
		}
		if kind.Kind == "empty_section" || kind.Kind == "u64_max" {
			var vector primitiveLuaVector
			if err := json.Unmarshal(raw, &vector); err != nil {
				t.Fatal(err)
			}
			fixture.Cases = append(fixture.Cases, vector)
		}
	}
	fields, _ := primitiveLuaRecord(vm, Record{{Name: "a", Value: []byte("b")}, {Name: "x", Value: nil}})
	single, _ := primitiveLuaRecord(vm, Record{{Name: "a", Value: []byte("b")}})
	for key, value := range map[string]lua.LValue{
		"u64_1_hex":   primitiveLuaOK(t, vm, "u64", lua.LNumber(1)),
		"f_A_hex":     primitiveLuaOK(t, vm, "frame", lua.LString("A")),
		"record_hex":  primitiveLuaOK(t, vm, "record", fields),
		"section_hex": primitiveLuaOK(t, vm, "section", lua.LString("s"), primitiveLuaArray(vm, single)),
	} {
		if hex.EncodeToString([]byte(primitiveLuaString(t, value))) != fixture.Expected.Framing[key] {
			t.Fatalf("shared primitive %s mismatch", key)
		}
	}
	if len(fixture.Cases) != 8 {
		t.Fatalf("expected 7 empty sections + full-width U64 boundary, got %d", len(fixture.Cases))
	}
	for _, tc := range fixture.Cases {
		if tc.Kind == "u64_max" {
			// The shared Go U64 vector covers all uint64. Lua deliberately rejects
			// this value rather than rounding it into the exact-integer domain.
			encoded, err := hex.DecodeString(tc.Expected["u64_hex"])
			if err != nil || !bytes.Equal(encoded, U64(math.MaxUint64)) {
				t.Fatal("invalid shared full-width U64 vector")
			}
			primitiveLuaError(t, vm, "parse_decimal", "INVALID_NUMBER", lua.LString(tc.Input["decimal"]))
			primitiveLuaError(t, vm, "decode_u64", "INVALID_NUMBER", lua.LString(encoded))
			continue
		}
		encoded := primitiveLuaOK(t, vm, "section", lua.LString(tc.Input["label"]), primitiveLuaArray(vm))
		goBytes, err := EncodeSection(tc.Input["label"], nil)
		if err != nil || encoded != lua.LString(goBytes) || hex.EncodeToString(goBytes) != tc.Expected["section_hex"] ||
			primitiveLuaOK(t, vm, "sha256", encoded) != lua.LString(tc.Expected["section_sha256"]) {
			t.Fatalf("shared section %s mismatch", tc.Name)
		}
	}
}

func TestPrimitiveLuaRecordRejections(t *testing.T) {
	t.Parallel()
	vm := primitiveLuaNew(t, true)
	record := Record{{Name: "b", Value: []byte("first")}, {Name: "a", Value: []byte(strings.Repeat("x", 128))}}
	fields, names := primitiveLuaRecord(vm, record)
	encoded := primitiveLuaEncoded(t, record)
	// The 128-byte value length contains a non-UTF8 prefix byte; only values
	// are text, not the record argument as a whole.
	if utf8.Valid(encoded) {
		t.Fatal("test must contain a non-UTF8 binary length prefix")
	}
	decoded := primitiveLuaOK(t, vm, "decode_record", lua.LString(encoded), names)
	if primitiveLuaOK(t, vm, "record", decoded) != lua.LString(encoded) {
		t.Fatal("text record with binary prefixes failed")
	}
	goDecoded, err := decodeExactRecord(encoded, []string{"b", "a"}, len(encoded))
	if err != nil || !bytes.Equal(primitiveLuaEncoded(t, goDecoded), encoded) {
		t.Fatal("Go decoder disagrees")
	}
	for i := 0; i < len(encoded); i++ {
		primitiveLuaError(t, vm, "decode_record", "INVALID_ENCODING", lua.LString(encoded[:i]), names)
	}
	for _, bad := range [][]byte{
		append(append([]byte(nil), encoded...), 0),
		append(U64(1), encoded[8:]...), append(U64(3), encoded[8:]...),
		primitiveLuaEncoded(t, Record{record[1], record[0]}),
		append(append(append(append(U64(2), F([]byte("b"))...), F(nil)...), F([]byte("b"))...), F(nil)...),
	} {
		if _, err := decodeExactRecord(bad, []string{"b", "a"}, primitiveLuaMaxBytes); err == nil {
			t.Fatal("Go decoder accepted malformed record")
		}
		primitiveLuaError(t, vm, "decode_record", "INVALID_ENCODING", lua.LString(bad), names)
	}
	// Exercise hostile lengths inside records, not just at the outer F boundary.
	primitiveLuaError(t, vm, "decode_record", "INVALID_NUMBER", lua.LString(append(U64(2), U64(math.MaxUint64)...)), names)
	primitiveLuaError(t, vm, "decode_record", "INVALID_ENCODING", lua.LString(append(U64(2), U64(MaxExactInteger)...)), names)
	primitiveLuaError(t, vm, "decode_record", "LIMIT_EXCEEDED", lua.LString(U64(primitiveLuaMaxFields+1)), names)
	primitiveLuaError(t, vm, "decode_record", "INVALID_NUMBER", lua.LString(U64(math.MaxUint64)), names)
	primitiveLuaError(t, vm, "decode_record", "LIMIT_EXCEEDED", lua.LString(encoded), names, lua.LNumber(len(encoded)-1))
	primitiveLuaError(t, vm, "record", "LIMIT_EXCEEDED", fields, lua.LNumber(len(encoded)-1))
	if primitiveLuaOK(t, vm, "record", fields, lua.LNumber(len(encoded))) != lua.LString(encoded) {
		t.Fatal("exact record bound rejected")
	}
	for _, name := range []string{"", "\x00", "\n", "\x7f", "é", "\xff"} {
		bad, badNames := primitiveLuaRecord(vm, Record{{Name: name, Value: []byte("text")}})
		primitiveLuaError(t, vm, "record", "INVALID_NAME", bad)
		primitiveLuaError(t, vm, "section", "INVALID_NAME", lua.LString(name), primitiveLuaArray(vm))
		primitiveLuaError(t, vm, "decode_record", "INVALID_NAME", lua.LString(U64(1)), badNames)
	}
	duplicate, duplicateNames := primitiveLuaRecord(vm, Record{{Name: "a"}, {Name: "a"}})
	primitiveLuaError(t, vm, "record", "DUPLICATE_NAME", duplicate)
	primitiveLuaError(t, vm, "decode_record", "DUPLICATE_NAME", lua.LString(U64(2)), duplicateNames)
	primitiveLuaError(t, vm, "decode_record", "INVALID_ARGUMENT", lua.LString(encoded), names, lua.LNil, lua.LString("false"))
	binaryFields, binaryNames := primitiveLuaRecord(vm, Record{{Name: "x", Value: []byte{0xff}}})
	binaryEncoded := primitiveLuaOK(t, vm, "record", binaryFields)
	primitiveLuaError(t, vm, "decode_record", "INVALID_UTF8", binaryEncoded, binaryNames)
	primitiveLuaOK(t, vm, "decode_record", binaryEncoded, binaryNames, lua.LNil, lua.LFalse)
}

func TestPrimitiveLuaUTF8(t *testing.T) {
	t.Parallel()
	vm := primitiveLuaNew(t, true)
	values := []string{"", "plain", "é雪😀", "\u007f\u0080\u009f", "\u00a0", "\u07ff\u0800\ud7ff\ue000\uffff\U00010000\U0010ffff", "\ufeff", "e\u0301", "\x80", "\xbf", "\xc0\x80", "\xc1\xbf", "\xc2", "\xc2A", "\xe0\x80\x80", "\xe0\x9f\xbf", "\xed\xa0\x80", "\xed\xbf\xbf", "\xef\xbf", "\xf0\x80\x80\x80", "\xf0\x8f\xbf\xbf", "\xf4\x90\x80\x80", "\xf5\x80\x80\x80", "\xff"}
	for i := 0; i < 256; i++ {
		values = append(values, string([]byte{byte(i)}), string(rune(i)))
	}
	rng := rand.New(rand.NewSource(8))
	for range 512 {
		value := make([]byte, rng.Intn(12))
		if _, err := rng.Read(value); err != nil {
			t.Fatal(err)
		}
		values = append(values, string(value))
	}
	for _, value := range values {
		if !utf8.ValidString(value) {
			primitiveLuaError(t, vm, "validate_text", "INVALID_UTF8", lua.LString(value))
			continue
		}
		if primitiveLuaOK(t, vm, "validate_text", lua.LString(value)) != lua.LTrue {
			t.Fatal("valid UTF-8 rejected")
		}
		if strings.ContainsFunc(value, unicode.IsControl) {
			primitiveLuaError(t, vm, "validate_text", "CONTROL_CHARACTER", lua.LString(value), lua.LTrue)
		} else if primitiveLuaOK(t, vm, "validate_text", lua.LString(value), lua.LTrue) != lua.LTrue {
			t.Fatal("non-control UTF-8 rejected")
		}
	}
	primitiveLuaError(t, vm, "validate_text", "INVALID_ARGUMENT", lua.LNumber(1))
	primitiveLuaError(t, vm, "validate_text", "INVALID_ARGUMENT", lua.LString("x"), lua.LString("false"))
	controlFields, controlNames := primitiveLuaRecord(vm, Record{{Name: "text", Value: []byte("\x00\n\u0085")}})
	primitiveLuaOK(t, vm, "decode_record", primitiveLuaOK(t, vm, "record", controlFields), controlNames)
}

func TestPrimitiveLuaArrayShapesAndBounds(t *testing.T) {
	t.Parallel()
	vm := primitiveLuaNew(t, true)
	for _, key := range []lua.LValue{lua.LString("1"), lua.LString("extra"), lua.LNumber(0), lua.LNumber(-1), lua.LNumber(1.5)} {
		array := primitiveLuaArray(vm)
		array.RawSet(key, lua.LString("x"))
		primitiveLuaError(t, vm, "record", "INVALID_ARRAY", array)
		primitiveLuaError(t, vm, "section", "INVALID_ARRAY", lua.LString("s"), array)
		primitiveLuaError(t, vm, "decode_record", "INVALID_ARRAY", lua.LString(U64(0)), array)
		primitiveLuaError(t, vm, "time_ms", "INVALID_TIME", array)
		reply := primitiveLuaArray(vm, lua.LString("1"), lua.LString("2"))
		reply.RawSet(key, lua.LString("hidden extra"))
		primitiveLuaError(t, vm, "time_ms", "INVALID_TIME", reply)
	}
	sparse := primitiveLuaArray(vm, lua.LString("1"), lua.LNil, lua.LString("3"))
	primitiveLuaError(t, vm, "record", "INVALID_ARRAY", sparse)
	metatable := vm.state.NewTable()
	metatable.RawSetString("__index", vm.state.NewFunction(func(l *lua.LState) int {
		l.RaiseError("metamethod must never execute")
		return 0
	}))
	withMeta := primitiveLuaArray(vm, lua.LString("1"), lua.LString("2"))
	vm.state.SetMetatable(withMeta, metatable)
	primitiveLuaError(t, vm, "record", "INVALID_ARRAY", withMeta)
	primitiveLuaError(t, vm, "time_ms", "INVALID_TIME", withMeta)
	primitiveLuaError(t, vm, "record", "INVALID_ARRAY", primitiveLuaArray(vm, withMeta))
	primitiveLuaError(t, vm, "record", "INVALID_ARGUMENT", primitiveLuaArray(vm, primitiveLuaArray(vm, lua.LString("a"), lua.LNumber(1))))
	primitiveLuaError(t, vm, "record", "INVALID_ARRAY", primitiveLuaArray(vm, primitiveLuaArray(vm, lua.LString("a"))))
	for _, value := range []lua.LValue{lua.LNil, lua.LFalse, lua.LString("{}"), lua.LNumber(0)} {
		primitiveLuaError(t, vm, "record", "INVALID_ARRAY", value)
		primitiveLuaError(t, vm, "section", "INVALID_ARRAY", lua.LString("s"), value)
	}
	fields := make(Record, primitiveLuaMaxFields)
	for i := range fields {
		fields[i].Name = fmt.Sprintf("f%d", i)
	}
	array, names := primitiveLuaRecord(vm, fields)
	encoded := primitiveLuaOK(t, vm, "record", array)
	primitiveLuaOK(t, vm, "decode_record", encoded, names)
	array.RawSetInt(primitiveLuaMaxFields+1, primitiveLuaArray(vm, lua.LString("extra"), lua.LString("")))
	primitiveLuaError(t, vm, "record", "LIMIT_EXCEEDED", array)
	array.RawSet(lua.LNumber(MaxExactInteger), lua.LString("hostile index"))
	primitiveLuaError(t, vm, "record", "LIMIT_EXCEEDED", array)
	records := primitiveLuaArray(vm)
	empty := primitiveLuaArray(vm)
	for i := 1; i <= primitiveLuaMaxRecords; i++ {
		records.RawSetInt(i, empty)
	}
	section := primitiveLuaOK(t, vm, "section", lua.LString("s"), records)
	if len(primitiveLuaString(t, section)) != 17+16*primitiveLuaMaxRecords {
		t.Fatal("maximum collection encoding mismatch")
	}
	primitiveLuaError(t, vm, "section", "LIMIT_EXCEEDED", lua.LString("s"), records, lua.LNumber(17+16*primitiveLuaMaxRecords-1))
	records.RawSetInt(primitiveLuaMaxRecords+1, empty)
	primitiveLuaError(t, vm, "section", "LIMIT_EXCEEDED", lua.LString("s"), records)
	for _, limit := range []lua.LValue{lua.LString("8"), lua.LNumber(-1), lua.LNumber(0.5), lua.LNumber(math.NaN()), lua.LNumber(math.Inf(1)), lua.LNumber(primitiveLuaMaxBytes + 1)} {
		primitiveLuaError(t, vm, "frame", "INVALID_ARGUMENT", lua.LString(""), limit)
		primitiveLuaError(t, vm, "record", "INVALID_ARGUMENT", empty, limit)
		primitiveLuaError(t, vm, "section", "INVALID_ARGUMENT", lua.LString("s"), empty, limit)
		primitiveLuaError(t, vm, "decode_record", "INVALID_ARGUMENT", lua.LString(U64(0)), empty, limit)
	}
	primitiveLuaError(t, vm, "record", "LIMIT_EXCEEDED", empty, lua.LNumber(7))
	primitiveLuaError(t, vm, "frame", "LIMIT_EXCEEDED", lua.LString(""), lua.LNumber(0))
	large := lua.LString(strings.Repeat("x", primitiveLuaMaxBytes+1))
	primitiveLuaError(t, vm, "validate_text", "LIMIT_EXCEEDED", large)
	primitiveLuaError(t, vm, "decode_record", "LIMIT_EXCEEDED", large, empty)
	primitiveLuaError(t, vm, "decode_frame", "LIMIT_EXCEEDED", large)
	primitiveLuaError(t, vm, "frame", "LIMIT_EXCEEDED", large)
	primitiveLuaError(t, vm, "record", "LIMIT_EXCEEDED", primitiveLuaArray(vm, primitiveLuaArray(vm, lua.LString("a"), large)))
	// The byte ceiling includes framing overhead, checked before output creation.
	atLimit := lua.LString(string(large)[:primitiveLuaMaxBytes-8])
	framed := primitiveLuaOK(t, vm, "frame", atLimit)
	if framed != lua.LString(F([]byte(atLimit))) || primitiveLuaOK(t, vm, "decode_frame", framed) != atLimit {
		t.Fatal("maximum frame mismatch")
	}
	primitiveLuaError(t, vm, "frame", "LIMIT_EXCEEDED", lua.LString(string(large)[:primitiveLuaMaxBytes-7]))
}
