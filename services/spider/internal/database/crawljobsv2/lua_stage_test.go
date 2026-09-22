package crawljobsv2

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// Pure module/receipt tests only. There is no Stage operation implementation,
// service, Redis process, runtime registration, or replacement Lua validator.
func stageLuaNew(t *testing.T) *jobLuaVM {
	t.Helper()
	vm := jobLuaNew(t) // also pins the Go oracle to 1.25.13
	vm.load(t, "StageOutput", "stage_output")
	vm.load(t, "Stage", "ledger_stage")
	return vm
}

func stageLuaCompare(t *testing.T, vm *jobLuaVM, schema RecordSchema, record Record) bool {
	t.Helper()
	want := ValidateRecord(schema, record)
	got, code := vm.invoke(t, "Schemas", "decode", lua.LString(schema), jobLuaEncoded(t, record))
	if (got != lua.LNil) != (want == nil) {
		t.Fatalf("schema %s Lua=%v/%v Go=%v fields=%v", schema, got, code, want, record)
	}
	if got == lua.LNil {
		if _, err := ParseErrorCode(code.String()); err != nil {
			t.Fatalf("nonclosed rejection %v", code)
		}
		return false
	}
	encoded, err := vm.invoke(t, "Schemas", "encode", got)
	if err != lua.LNil || encoded != jobLuaEncoded(t, record) {
		t.Fatal("roundtrip changed bytes", err)
	}
	return true
}

func stageLuaPage() Record {
	return Record{textField("normalized_url", "https://example.com/path"), textField("html", "<p>é\x00</p>"),
		textField("original_html", ""), textField("content_type", "text/html; charset=utf-8"), textField("status_code", "200"),
		textField("last_crawled", "Thu, 01 Jan 1970 00:00:00 UTC"), textField("rendered", "false"),
		textField("render_policy_rule", ""), textField("render_policy_sha256", ""), textField("publication_id", strings.Repeat("a", 64))}
}

func TestStageLuaSchemaEveryFieldEveryState(t *testing.T) {
	t.Parallel()
	vm := stageLuaNew(t)
	page := stageLuaPage()
	rendered := cloneRecord(page)
	rendered[2].Value, rendered[6].Value, rendered[7].Value, rendered[8].Value = []byte("source"), []byte("true"), []byte("é-rule"), []byte(strings.Repeat("a", 64))
	image, _ := NewFinalImageRecord(Digest(strings.Repeat("a", 64)), "https://example.com/path", OutputImage{NormalizedSourceURL: "https://example.com/pic%2Fname?q=%FF", Alt: "\x00é\u0085"})
	ir, _ := image.Record()
	manifest, _ := NewImageManifestRecord(Digest(strings.Repeat("a", 64)), "https://example.com/path", nil)
	mr, _ := manifest.Record()
	fixtures := []struct {
		schema RecordSchema
		record Record
	}{{SchemaStageMeta, recordAuthorityUnsealedStageRecord(t, "0")}, {SchemaStageMeta, recordAuthorityUnsealedStageRecord(t, "1")},
		{SchemaStageMeta, recordAuthoritySealedStageRecord(t, "0")}, {SchemaStageMeta, recordAuthoritySealedStageRecord(t, "1")},
		{SchemaFinalPage, page}, {SchemaFinalPage, rendered}, {SchemaFinalImage, ir}, {SchemaImageManifest, mr}}
	for fixture, test := range fixtures {
		if !stageLuaCompare(t, vm, test.schema, test.record) {
			t.Fatal("bad baseline")
		}
		for i, f := range test.record {
			for _, value := range []string{"", "0", "1", "00", "-1", "9007199254740991", "9007199254740992", "\xff", "\x00", strings.Repeat("0", 64), strings.Repeat("f", 64)} {
				t.Run(fmt.Sprintf("%d/%s/%x", fixture, f.Name, value), func(t *testing.T) {
					r := cloneRecord(test.record)
					r[i].Value = []byte(value)
					stageLuaCompare(t, vm, test.schema, r)
				})
			}
			missing := append(cloneRecord(test.record[:i]), cloneRecord(test.record[i+1:])...)
			stageLuaCompare(t, vm, test.schema, missing)
			wrong := cloneRecord(test.record)
			wrong[i].Name += "_wrong"
			stageLuaCompare(t, vm, test.schema, wrong)
		}
		stageLuaCompare(t, vm, test.schema, append(cloneRecord(test.record), textField("extra", "")))
	}
}

func TestStageLuaMetaProgressAndExactIntegerEdges(t *testing.T) {
	t.Parallel()
	vm := stageLuaNew(t)
	for _, sealed := range []bool{false, true} {
		for _, abandoned := range []string{"0", "1"} {
			for _, count := range []uint64{0, 1, 63, 64, 65, 127, 128, 255, 256} {
				r := recordAuthoritySealedStageRecord(t, abandoned)
				for index, value := range map[int]string{stageExpectedOutlinksIndex: canonicalDecimal(count), stageOutlinksWrittenIndex: canonicalDecimal(count)} {
					r[index].Value = []byte(value)
				}
				for i := uint64(0); i < 4; i++ {
					v := ""
					if i < (count+63)/64 {
						v = strings.Repeat("a", 64)
					}
					r[stageOutlinksChunk0DigestIndex+int(i)].Value = []byte(v)
				}
				if count == 0 {
					r[stageKeyCountIndex].Value = []byte("9")
				}
				if !sealed {
					r[stageSealedIndex].Value, r[stageSealedAtMSIndex].Value = []byte("0"), []byte("0")
				}
				stageLuaCompare(t, vm, SchemaStageMeta, r)
			}
		}
	}
	for _, created := range []uint64{0, 1, MaxExactInteger - StageTTLMilliseconds, MaxExactInteger - StageTTLMilliseconds + 1} {
		r := recordAuthorityUnsealedStageRecord(t, "0")
		r[stageCreatedAtMSIndex].Value = []byte(canonicalDecimal(created))
		r[stageExpiresAtMSIndex].Value = []byte(canonicalDecimal(created + StageTTLMilliseconds))
		stageLuaCompare(t, vm, SchemaStageMeta, r)
	}
}

func TestStageLuaContentTypeGoMimeExact(t *testing.T) {
	t.Parallel()
	vm := stageLuaNew(t)
	corpus := []string{"text/html", "TEXT/HTML", "Text/Html;charset=UTF-8", "text/html;", "text/html ;charset = \"utf-8\";",
		"text/html; charset=utf-8;charset=utf-8", "text/html;charset=utf-8;charset=UTF-8", "text/html;charset=\"utf\\-8\"",
		"text/html;charset*=utf-8''utf%2D8", "text/html;charset*0=utf-;charset*1=8", "text/html;charset*0*=UTF-8'en'utf-;charset*1*=8",
		"text/html;ignored*=bad''x", "text/html;ignored*9=x", "text/html;charset*=utf-8''%FF", "text/html;charset*=US-ASCII''UTF-8",
		"text/html; charset*0*=bad''x; charset*1=utf-8", "text/html;charset*0=utf-8; charset*1*=%GG",
		"text/html; charset=\"utf-8\r\"", "text/html; charset=\"utf-8\n\"", "text/html;charset=\"utf-8\x00\"", "text/html;x=y",
		"text/html;;", "text /html", "text/html;charset=", "text/html;charset=\"\"", "text/html\u00a0;charset=utf-8"}
	for _, space := range []string{" ", "\t", "\n", "\r", "\v", "\f", "\u0085", "\u00a0", "\u1680", "\u2000", "\u2028", "\u2029", "\u202f", "\u205f", "\u3000", "\u200b", "\ufeff"} {
		corpus = append(corpus, space+"text/html", "text/html"+space, "text/html;"+space+"charset"+space+"="+space+"utf-8")
	}
	rng := rand.New(rand.NewSource(47))
	for i := 0; i < 300; i++ {
		base := corpus[rng.Intn(len(corpus))]
		at := rng.Intn(len(base) + 1)
		corpus = append(corpus, base[:at]+string([]byte{byte(rng.Intn(256))})+base[at:])
	}
	for _, value := range corpus {
		want := validateContentType(value)
		got, code := vm.invoke(t, "StageOutput", "content_type", lua.LString(value))
		if (got != lua.LNil) != (want == nil) {
			t.Fatalf("%q Lua=%v/%v Go=%v", value, got, code, want)
		}
	}
}

func TestStageLuaTimestampNoClockAndUTF8Controls(t *testing.T) {
	t.Parallel()
	vm := stageLuaNew(t)
	times := []uint64{0, 1, 999, 1000, 86400000 - 1, 86400000, MaxExactInteger - 1, MaxExactInteger}
	for _, stamp := range []string{"1970-01-01T00:00:00Z", "1999-12-31T23:59:59Z", "2000-02-29T23:59:59Z", "2100-03-01T00:00:00Z", "2400-02-29T00:00:00Z", "9999-12-31T23:59:59Z"} {
		v, _ := time.Parse(time.RFC3339, stamp)
		times = append(times, uint64(v.UnixMilli()), uint64(v.UnixMilli())+999, uint64(v.UnixMilli())+1000)
	}
	for _, ms := range times {
		got, code := vm.invoke(t, "StageOutput", "last_crawled", lua.LString(canonicalDecimal(ms)))
		if code != lua.LNil || got != lua.LString(formatRedisLastCrawled(RedisMilliseconds(ms))) {
			t.Fatalf("%d: %v/%v", ms, got, code)
		}
	}
	for _, value := range []string{"-1", "01", "9007199254740992", "1.0", ""} {
		if got, _ := vm.invoke(t, "StageOutput", "last_crawled", lua.LString(value)); got != lua.LNil {
			t.Fatal("bad millisecond accepted")
		}
	}
	for _, stamp := range []string{"Sat, 01 Jan 0000 00:00:00 UTC", "Fri, 31 Dec 9999 23:59:59 UTC", "Thu, 29 Feb 1900 12:00:00 UTC", "Tue, 29 Feb 2000 23:59:60 UTC", "1970-01-01T00:00:00Z", "Fri, 01 Jan 1970 00:00:00 UTC"} {
		r := stageLuaPage()
		r[5].Value = []byte(stamp)
		stageLuaCompare(t, vm, SchemaFinalPage, r)
	}
	for _, cp := range []rune{0, 9, 31, 32, 127, 128, 159, 160, 0x200b, 0x2028, 0xfeff, 0x1f4a9} {
		r := stageLuaPage()
		r[2].Value, r[6].Value, r[7].Value, r[8].Value = []byte("x"), []byte("true"), []byte("rule"+string(cp)), []byte(strings.Repeat("a", 64))
		stageLuaCompare(t, vm, SchemaFinalPage, r)
	}
}

func TestStageLuaOutputKeysManifestAndBounds(t *testing.T) {
	t.Parallel()
	vm := stageLuaNew(t)
	pub := Digest(strings.Repeat("a", 64))
	urls := []string{"https://example.com/", "https://xn--bcher-kva.example/%C3%A9?q=%FF", "https://example.com/pic%2Fname%25?q=a+b", "http://127.0.0.1/", "http://[2001:db8::1]/", "https://example.com/" + strings.Repeat("x", 2048-len("https://example.com/"))}
	for _, page := range urls {
		for _, source := range urls {
			keys, code := vm.invoke(t, "StageOutput", "keys", lua.LString(pub), lua.LString(page), lua.LString(source))
			want, err := ImageDataKey(pub, page, source)
			if err != nil || code != lua.LNil || keys.(*lua.LTable).RawGetString("image") != lua.LString(want) {
				t.Fatalf("key mismatch: %v/%v/%v", keys, code, err)
			}
		}
	}
	for _, count := range []int{0, 1, 64} {
		images := make([]OutputImage, count)
		for i := range images {
			images[i] = OutputImage{NormalizedSourceURL: fmt.Sprintf("https://example.com/%02d", i), Alt: strings.Repeat("é", 512)}
		}
		m, err := NewImageManifestRecord(pub, urls[5], images)
		if err != nil {
			t.Fatal(err)
		}
		r, _ := m.Record()
		stageLuaCompare(t, vm, SchemaImageManifest, r)
		for _, bad := range []string{"null", "[ ]", "[]\n", "[null]", "[\"image_data:../outside\"]", string(r[4].Value) + " ", strings.ReplaceAll(string(r[4].Value), "image_data", `image\u005fdata`)} {
			candidate := cloneRecord(r)
			candidate[4].Value = []byte(bad)
			stageLuaCompare(t, vm, SchemaImageManifest, candidate)
		}
	}
	for _, length := range []int{MaxPageBlobBytes, MaxPageBlobBytes + 1} {
		r := stageLuaPage()
		r[1].Value = []byte(strings.Repeat("x", length))
		stageLuaCompare(t, vm, SchemaFinalPage, r)
	}
	for _, length := range []int{128, 129} {
		r := stageLuaPage()
		r[2].Value, r[6].Value, r[7].Value, r[8].Value = []byte("x"), []byte("true"), []byte(strings.Repeat("x", length)), []byte(strings.Repeat("a", 64))
		stageLuaCompare(t, vm, SchemaFinalPage, r)
	}
	image, _ := NewFinalImageRecord(pub, urls[0], OutputImage{NormalizedSourceURL: urls[1]})
	ir, _ := image.Record()
	for _, alt := range []string{strings.Repeat("é", 512), strings.Repeat("é", 512) + "x", "\xff"} {
		r := cloneRecord(ir)
		r[4].Value = []byte(alt)
		stageLuaCompare(t, vm, SchemaFinalImage, r)
	}
	for _, bad := range []string{"https://EXAMPLE.com/", "https://example.com:443/", "https://bücher.example/", "https://example.com/%2f", "https://xn--a.example/", "https://example.com/" + strings.Repeat("x", 2048), "https://example.com/\xff"} {
		_, want := ImageDataKey(pub, urls[0], bad)
		got, code := vm.invoke(t, "StageOutput", "keys", lua.LString(pub), lua.LString(urls[0]), lua.LString(bad))
		if (got != lua.LNil) != (want == nil) {
			t.Fatalf("image normalization %q: %v/%v Go=%v", bad, got, code, want)
		}
	}
	for _, count := range []int{64, 65} {
		keys := make([]string, count)
		for i := range keys {
			keys[i], _ = ImageDataKey(pub, urls[0], fmt.Sprintf("https://example.com/%02d", i))
		}
		encoded, _ := json.Marshal(keys)
		r := Record{textField("contract_version", "1"), textField("publication_id", string(pub)), textField("normalized_url", urls[0]), textField("image_count", strconv.Itoa(count)), textField("image_keys", string(encoded))}
		stageLuaCompare(t, vm, SchemaImageManifest, r)
		r[4].Value = []byte(strings.Repeat("x", MaxImageManifestBytes+1))
		stageLuaCompare(t, vm, SchemaImageManifest, r)
	}
}

// Extends the existing read-only command facade by LIST/PTTL. NO mutation
// commands are accepted. Tests materialize fixtures outside Lua, not real ops.
type stageLuaRedis struct {
	*jobLuaRedis
	lists map[string][]string
	ttls  map[string]int64
}

func (r *stageLuaRedis) call(l *lua.LState) int {
	cmd := l.CheckString(1)
	if cmd == "TIME" {
		return r.jobLuaRedis.call(l)
	}
	key := l.CheckString(2)
	if cmd == "TYPE" && r.lists[key] != nil {
		r.trace = append(r.trace, cmd)
		v := l.NewTable()
		v.RawSetString("ok", lua.LString("list"))
		l.Push(v)
		return 1
	}
	switch cmd {
	case "LLEN":
		l.Push(lua.LNumber(len(r.lists[key])))
	case "LRANGE":
		start, _ := strconv.ParseInt(l.CheckString(3), 10, 64)
		end, _ := strconv.ParseInt(l.CheckString(4), 10, 64)
		l.Push(bootLuaStrings(l, sharedLuaRange(r.lists[key], start, end)))
	case "PTTL":
		n, ok := r.ttls[key]
		if !ok {
			n = -1
		}
		l.Push(lua.LNumber(n))
	default:
		return r.jobLuaRedis.call(l)
	}
	r.trace = append(r.trace, cmd)
	return 1
}

type stageLuaFixture struct {
	vm                        *jobLuaVM
	r                         *stageLuaRedis
	run                       *lua.LTable
	runRecord, job, meta      Record
	lease                     LeaseIdentity
	commit, publication       Digest
	base, jobKey, stagePrefix string
	allKeys                   []string
	stageKeys                 []string
}

func stageLuaFixtureNew(t *testing.T) *stageLuaFixture {
	t.Helper()
	return stageLuaFixtureStarts(t, 1)
}

func stageLuaFixtureStarts(t *testing.T, starts uint64) *stageLuaFixture {
	t.Helper()
	vm := stageLuaNew(t)
	source := jobLuaSourceValue(t, "https://example.com/path")
	group := jobLuaGroup(source)
	run, runRecord := jobLuaBinding(t, vm, []PolicyGroup{group})
	if starts > 1 {
		runRecord[runRequestStartsIndex].Value = []byte(canonicalDecimal(starts + 2))
		runRecord[runReservationCreationsTotalIndex].Value = []byte(canonicalDecimal(starts + 2))
		run.RawSetString("v", jobLuaValues(vm, runRecord))
	}
	r := &stageLuaRedis{jobLuaRedis: jobLuaStore(), lists: map[string][]string{}, ttls: map[string]int64{}}
	r.nowMS = 500
	lease := LeaseIdentity{RunID: RunID(strings.Repeat("1", 32)), JobID: source.JobID, OwnerID: OwnerID(strings.Repeat("3", 32)), Fence: 1, Token: LeaseToken(strings.Repeat("4", 64))}
	output := Digest(strings.Repeat("7", 64))
	pub, _ := DerivePublicationID(PublicationIdentity{RunID: lease.RunID, JobID: lease.JobID, Fence: lease.Fence, OutputDigest: output})
	commit, _ := DeriveCommitID(CommitIdentity{RunID: lease.RunID, JobID: lease.JobID, OwnerID: lease.OwnerID, Fence: lease.Fence, Token: lease.Token, PublicationID: pub, RequestStartsBaseline: 0, RequestStartsGeneration: starts})
	token, _ := DeriveTokenDigest(lease)
	job := recordAuthorityJobRecord(t, "leased")
	sourceRecord, _ := completeSourceJobRecord(source)
	jobLuaApplySource(job, sourceRecord)
	target, _ := DeriveTargetDigest(RequestTarget{URLID: source.JobID, CanonicalURL: source.CanonicalURL})
	for i, v := range map[int]string{jobActiveReservationIDIndex: "", jobActiveStageCommitIDIndex: string(commit), jobLastStageCommitIDIndex: string(commit), jobLastStageFenceIndex: "1",
		jobRequestStartsIndex: canonicalDecimal(starts), jobNextRequestOrdinalIndex: canonicalDecimal(starts + 1),
		jobLastDocumentRequestFenceIndex: "1", jobLastDocumentRequestStartedAtMSIndex: "150", jobLastDocumentTargetURLIDIndex: string(source.JobID), jobLastDocumentTargetURLIndex: source.CanonicalURL, jobLastDocumentTargetDigestIndex: string(target)} {
		job[i].Value = []byte(v)
	}
	if err := ValidateRecord(SchemaJob, job); err != nil {
		t.Fatal(err)
	}
	meta := recordAuthorityUnsealedStageRecord(t, "0")
	for i, v := range map[int]string{stageJobIDIndex: string(source.JobID), stageCommitIDIndex: string(commit), stagePublicationIDIndex: string(pub), stageTokenDigestIndex: string(token), stageCreatedAtMSIndex: "500", stageExpiresAtMSIndex: "900500", stageRequestStartsGenerationIndex: canonicalDecimal(starts)} {
		meta[i].Value = []byte(v)
	}
	base := "mifolyo:crawl:v2:run:" + string(lease.RunID)
	f := &stageLuaFixture{vm: vm, r: r, run: run, runRecord: runRecord, job: job, meta: meta, lease: lease, commit: commit, publication: pub, base: base, jobKey: base + ":job:" + string(source.JobID), stagePrefix: "mifolyo:crawl:v2:stage:" + string(commit) + ":"}
	r.hash(base, runRecord)
	r.hash(f.jobKey, job)
	f.allKeys = []string{base, f.jobKey, StageSlotsKey, StageExpiryKey, ActiveLeasesKey}
	for name, value := range map[string]string{"group_limits": "10", "group_rate_scope_ids": string(group.RateScopeID), "group_scope_ids": string(group.GroupScopeID), "group_concurrency": "3", "group_interval_ms": "100"} {
		r.hashes[base+":"+name] = map[string]string{"default": value}
		f.allKeys = append(f.allKeys, base+":"+name)
	}
	for _, name := range []string{"jobs", "job_order", "ready", "ready_at", "leased", "leased_at", "delayed", "completed", "dead", "cancelled", "commit_backpressure"} {
		f.allKeys = append(f.allKeys, base+":"+name)
	}
	f.indices()
	f.stageKeys = wireOracleStageKeys(commit)
	f.allKeys = append(f.allKeys, f.stageKeys...)
	r.hash(f.stagePrefix+"meta", meta)
	r.lists[f.stagePrefix+"keys"] = []string{f.stagePrefix + "meta", f.stagePrefix + "keys"}
	r.ttls[f.stagePrefix+"meta"], r.ttls[f.stagePrefix+"keys"] = 900000, 900000
	r.hashes[StageSlotsKey] = map[string]string{string(commit): "50000000:" + string(lease.RunID) + ":" + string(lease.JobID) + ":1:0"}
	r.zsets[StageExpiryKey] = map[string]string{string(commit): "900500"}
	return f
}

func (f *stageLuaFixture) indices() {
	id := string(f.lease.JobID)
	f.r.sets[f.base+":jobs"] = map[string]bool{id: true}
	f.r.zsets[f.base+":job_order"] = map[string]string{id: "0"}
	for _, name := range []string{"leased", "leased_at", "completed", "commit_backpressure"} {
		delete(f.r.zsets, f.base+":"+name)
	}
	delete(f.r.zsets, ActiveLeasesKey)
	if string(f.job[jobStateIndex].Value) == "leased" {
		f.r.zsets[f.base+":leased"] = map[string]string{id: string(f.job[jobLeaseExpiresAtMSIndex].Value)}
		f.r.zsets[f.base+":leased_at"] = map[string]string{id: string(f.job[jobLeaseStartedAtMSIndex].Value)}
		f.r.zsets[ActiveLeasesKey] = map[string]string{string(f.lease.RunID) + ":" + id: string(f.job[jobLeaseExpiresAtMSIndex].Value)}
	} else if string(f.job[jobStateIndex].Value) == "completed" {
		f.r.zsets[f.base+":completed"] = map[string]string{id: string(f.job[jobCompletedAtMSIndex].Value)}
	}
	if string(f.job[jobCommitBackpressureStartedAtMSIndex].Value) != "0" {
		f.r.zsets[f.base+":commit_backpressure"] = map[string]string{id: string(f.job[jobCommitBackpressureStartedAtMSIndex].Value)}
	}
}

func (f *stageLuaFixture) open(t *testing.T, operation string, omit string) (*lua.LTable, *lua.LTable, *lua.LTable) {
	t.Helper()
	vm := f.vm
	ctx := jobLuaOpen(t, vm, f.r.jobLuaRedis, f.allKeys)
	// The foundation has no stage wire recipes. Set the operation selector for
	// pure validation only; this does NOT grant private Context read/write keys.
	ctx.RawSetString("operation", lua.LString(operation))
	redis := vm.state.NewTable()
	redis.RawSetString("call", vm.state.NewFunction(f.r.call))
	vm.env.RawSetString("redis", redis)
	read := func(method string, args ...lua.LValue) lua.LValue {
		all := append([]lua.LValue{ctx}, args...)
		got, code := vm.invoke(t, "Read", method, all...)
		if code != lua.LNil {
			t.Fatalf("read %s %v: %v", method, args, code)
		}
		return got
	}
	read("fixed_hash", lua.LString(f.base), lua.LString("run"))
	job := read("fixed_hash", lua.LString(f.jobKey), lua.LString("job")).(*lua.LTable)
	for _, name := range []string{"group_limits", "group_rate_scope_ids", "group_scope_ids", "group_concurrency", "group_interval_ms"} {
		read("dynamic_hash", lua.LString(f.base+":"+name), lua.LNumber(64), lua.LNumber(128), lua.LNumber(64))
	}
	for _, name := range []string{"jobs", "job_order", "ready", "ready_at", "leased", "leased_at", "delayed", "completed", "dead", "cancelled", "commit_backpressure", "active_leases"} {
		key, member, kind := f.base+":"+name, string(f.lease.JobID), "zset"
		if name == "jobs" {
			kind = "set"
		}
		if name == "active_leases" {
			key, member = ActiveLeasesKey, string(f.lease.RunID)+":"+member
		}
		if key != omit {
			read("members", lua.LString(key), lua.LString(kind), bootLuaStrings(vm.state, []string{member}), lua.LNumber(10000), lua.LNumber(128))
		}
	}
	if omit != StageSlotsKey {
		read("dynamic_hash", lua.LString(StageSlotsKey), lua.LNumber(4), lua.LNumber(64), lua.LNumber(126))
		read("hash_fields", lua.LString(StageSlotsKey), bootLuaStrings(vm.state, []string{string(f.commit)}), lua.LNumber(4), lua.LNumber(64), lua.LNumber(126))
	}
	if omit != StageExpiryKey {
		read("members", lua.LString(StageExpiryKey), lua.LString("zset"), bootLuaStrings(vm.state, []string{string(f.commit)}), lua.LNumber(100000), lua.LNumber(64))
	}
	for _, key := range f.stageKeys {
		if key == omit {
			continue
		}
		switch key {
		case f.stagePrefix + "keys":
			read("page", lua.LString(key), lua.LString("list"), lua.LNumber(0), lua.LNumber(73), lua.LNumber(73), lua.LNumber(128))
		case f.stagePrefix + "outlinks":
			read("all_members", lua.LString(key), lua.LString("set"), lua.LNumber(256), lua.LNumber(2048))
		case f.stagePrefix + "discoveries":
			read("all_members", lua.LString(key), lua.LString("zset"), lua.LNumber(128), lua.LNumber(64))
		default:
			// Dynamic meta deliberately also permits contextual COUNTER_CORRUPT
			// classification before a schema-only read rejects a stored tuple.
			read("dynamic_hash", lua.LString(key), lua.LNumber(896), lua.LNumber(128), lua.LNumber(MaxPageBlobBytes))
		}
		if key+":ttl" != omit {
			read("ttl", lua.LString(key))
		}
	}
	view := vm.state.NewTable()
	view.RawSetString("ctx", ctx)
	return ctx, view, job
}

func (f *stageLuaFixture) leaseValue() *lua.LTable {
	return jobLuaValues(f.vm, Record{textField("run_id", string(f.lease.RunID)), textField("job_id", string(f.lease.JobID)), textField("owner_id", string(f.lease.OwnerID)), textField("lease_token", string(f.lease.Token)), textField("fence", canonicalDecimal(uint64(f.lease.Fence)))})
}

func (f *stageLuaFixture) check(t *testing.T, view, job *lua.LTable, method string) (lua.LValue, lua.LValue) {
	t.Helper()
	before := len(f.r.trace)
	value, code := f.vm.invoke(t, "Stage", method, view, f.run, job, f.leaseValue(), lua.LString(f.commit))
	if before != len(f.r.trace) {
		t.Fatal("Stage performed I/O")
	}
	return value, code
}

func TestStageLuaOwnedReceiptsAndMutationIsolation(t *testing.T) {
	t.Parallel()
	f := stageLuaFixtureNew(t)
	ctx, view, job := f.open(t, "CJ2_STAGE_PAGE_FIELDS", "")
	stage, code := f.check(t, view, job, "check_owned")
	if code != lua.LNil {
		t.Fatal(code)
	}
	stage.(*lua.LTable).RawSetString("remaining", lua.LNumber(MaxExactInteger))
	p, code := f.vm.invoke(t, "Stage", "slot_proof", stage, ctx)
	if code != lua.LNil || p.(*lua.LTable).RawGetString("remaining") != lua.LNumber(50000000) {
		t.Fatal("editable public memory hint became authority", code)
	}
	if _, code := f.vm.invoke(t, "Stage", "slot_proof", f.vm.state.NewTable(), ctx); code != lua.LString("INVALID_STATE") {
		t.Fatal("fabricated proof accepted")
	}
	if _, code := f.vm.invoke(t, "Stage", "slot_proof", stage, f.vm.state.NewTable()); code != lua.LString("INVALID_STATE") {
		t.Fatal("foreign-context proof accepted")
	}
	for _, omit := range []string{StageSlotsKey, StageExpiryKey, f.stagePrefix + "meta", f.stagePrefix + "keys", f.stagePrefix + "page", f.stagePrefix + "image:63", f.stagePrefix + "meta:ttl", ActiveLeasesKey} {
		_, view, job := f.open(t, "CJ2_STAGE_PAGE_FIELDS", omit)
		if value, code := f.check(t, view, job, "check_owned"); value != lua.LNil || code == lua.LNil {
			t.Fatal("missing explicit receipt became absence", omit)
		}
	}
	for _, mutation := range []string{"B", "G", "fence", "owner", "token", "publication", "output", "slot-owner", "terminal-slot", "ttl-extension", "expiry", "inventory-path", "inventory-duplicate", "data-bytes", "source"} {
		t.Run(mutation, func(t *testing.T) {
			f := stageLuaFixtureNew(t)
			m := f.r.hashes[f.stagePrefix+"meta"]
			switch mutation {
			case "B":
				m["request_starts_baseline"] = "1"
			case "G":
				m["request_starts_generation"] = "2"
			case "fence":
				m["lease_fence"] = "2"
			case "owner":
				m["owner_id"] = strings.Repeat("a", 32)
			case "token":
				m["token_digest"] = strings.Repeat("a", 64)
			case "publication":
				m["publication_id"] = strings.Repeat("a", 64)
			case "output":
				m["output_digest"] = strings.Repeat("a", 64)
			case "slot-owner":
				f.r.hashes[StageSlotsKey][string(f.commit)] = "50000000:" + strings.Repeat("a", 32) + ":" + string(f.lease.JobID) + ":1:0"
			case "terminal-slot":
				f.r.hashes[StageSlotsKey][string(f.commit)] = strings.TrimSuffix(f.r.hashes[StageSlotsKey][string(f.commit)], ":0") + ":2"
			case "ttl-extension":
				f.r.ttls[f.stagePrefix+"meta"]++
			case "expiry":
				f.r.zsets[StageExpiryKey][string(f.commit)] = "900501"
			case "inventory-path":
				f.r.lists[f.stagePrefix+"keys"][0] = f.stagePrefix + "../victim"
			case "inventory-duplicate":
				f.r.lists[f.stagePrefix+"keys"][0] = f.stagePrefix + "keys"
			case "data-bytes":
				m["data_bytes"] = "1"
			case "source":
				f.job[jobPolicyDecisionSHA256Index].Value = []byte(strings.Repeat("f", 64))
				f.r.hash(f.jobKey, f.job)
			}
			_, view, job := f.open(t, "CJ2_STAGE_PAGE_FIELDS", "")
			value, code := f.check(t, view, job, "check_owned")
			if value != lua.LNil || code == lua.LNil {
				t.Fatal("mutation accepted")
			}
			if (mutation == "B" || mutation == "G") && code != lua.LString("COUNTER_CORRUPT") {
				t.Fatal("tuple drift misclassified", code)
			}
		})
	}
}

func (f *stageLuaFixture) input(t *testing.T, kind ChunkKind, ordinal uint64, records []Record) *lua.LTable {
	t.Helper()
	chunk, err := newValidatedStageChunk(f.commit, kind, ordinal, records)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := DeriveChunkDigest(chunk)
	if err != nil {
		t.Fatal(err)
	}
	v := f.leaseValue()
	for name, value := range map[string]string{"commit_id": string(f.commit), "chunk_kind": string(kind), "chunk_ordinal": canonicalDecimal(ordinal), "chunk_digest": string(digest), "record_count": strconv.Itoa(len(records))} {
		v.RawSetString(name, lua.LString(value))
	}
	input, array := f.vm.state.NewTable(), f.vm.state.NewTable()
	for i, r := range records {
		array.RawSetInt(i+1, jobLuaEncoded(t, r))
	}
	input.RawSetString("v", v)
	input.RawSetString("records", array)
	return input
}

func stageLuaRecord(p *lua.LTable) Record {
	fields := p.RawGetString("fields").(*lua.LTable)
	var r Record
	for i := 1; i <= fields.Len(); i++ {
		pair := fields.RawGetInt(i).(*lua.LTable)
		r = append(r, textField(pair.RawGetInt(1).String(), pair.RawGetInt(2).String()))
	}
	return r
}

func (f *stageLuaFixture) materialize(t *testing.T, prepared *lua.LTable) {
	t.Helper()
	targets := prepared.RawGetString("targets").(*lua.LTable)
	for i := 1; i <= targets.Len(); i++ {
		target := targets.RawGetInt(i).(*lua.LTable)
		key, kind := target.RawGetString("key").String(), target.RawGetString("kind").String()
		switch kind {
		case "hash":
			if f.r.hashes[key] == nil {
				f.r.hashes[key] = map[string]string{}
			}
			for _, field := range stageLuaRecord(target) {
				f.r.hashes[key][field.Name] = string(field.Value)
			}
		case "set":
			if f.r.sets[key] == nil {
				f.r.sets[key] = map[string]bool{}
			}
			f.r.sets[key][target.RawGetString("member").String()] = true
		case "zset":
			if f.r.zsets[key] == nil {
				f.r.zsets[key] = map[string]string{}
			}
			f.r.zsets[key][target.RawGetString("member").String()] = target.RawGetString("score").String()
		}
		f.r.ttls[key] = 900000
	}
	newKeys := prepared.RawGetString("new_keys").(*lua.LTable)
	for i := 1; i <= newKeys.Len(); i++ {
		f.r.lists[f.stagePrefix+"keys"] = append(f.r.lists[f.stagePrefix+"keys"], newKeys.RawGetInt(i).String())
	}
	f.meta = stageLuaRecord(prepared.RawGetString("next_meta").(*lua.LTable))
	if err := ValidateRecord(SchemaStageMeta, f.meta); err != nil {
		t.Fatal("prepared metadata disagrees with Go", err)
	}
	f.r.hash(f.stagePrefix+"meta", f.meta)
}

func (f *stageLuaFixture) prepare(t *testing.T, kind ChunkKind, ordinal uint64, records []Record, replay bool) *lua.LTable {
	t.Helper()
	operation := map[ChunkKind]string{ChunkPageFields: "CJ2_STAGE_PAGE_FIELDS", ChunkHTML: "CJ2_STAGE_PAGE_BLOB", ChunkOriginalHTML: "CJ2_STAGE_PAGE_BLOB", ChunkOutlinks: "CJ2_STAGE_OUTLINKS_BATCH", ChunkDiscoveries: "CJ2_STAGE_DISCOVERIES_BATCH", ChunkAliases: "CJ2_STAGE_ALIASES_BATCH", ChunkImages: "CJ2_STAGE_IMAGES_BATCH", ChunkImageManifest: "CJ2_STAGE_IMAGE_MANIFEST"}[kind]
	ctx, view, job := f.open(t, operation, "")
	stage, code := f.check(t, view, job, "check_owned")
	if code != lua.LNil {
		t.Fatalf("owned before %s: %v", kind, code)
	}
	input := f.input(t, kind, ordinal, records)
	before := len(f.r.trace)
	prepared, code := f.vm.invoke(t, "Stage", "validate_chunk", ctx, f.run, job, stage, input)
	if code != lua.LNil {
		t.Fatalf("chunk %s: %v", kind, code)
	}
	if len(f.r.trace) != before {
		t.Fatal("chunk validator performed I/O")
	}
	p := prepared.(*lua.LTable)
	if p.RawGetString("replay") != lua.LBool(replay) {
		t.Fatal("wrong replay classification")
	}
	if replay && (p.RawGetString("data_delta") != lua.LNumber(0) || p.RawGetString("key_delta") != lua.LNumber(0)) {
		t.Fatal("replay allocated")
	}
	// Public proof edits are irrelevant, but changing a supplied replay byte with
	// the SAME trusted digest must still reject (not merely compare the digest).
	if replay {
		changed := cloneRecord(records[0])
		switch kind {
		case ChunkHTML, ChunkOriginalHTML:
			changed[1].Value = append(changed[1].Value, 'x')
		case ChunkImages:
			changed[1].Value = []byte("different alt")
		case ChunkPageFields:
			changed[2].Value = []byte("201")
		case ChunkDiscoveries:
			changed[3].Value = []byte("0.1")
		default:
			changed[0].Value = append(changed[0].Value, 'x')
		}
		input.RawGetString("records").(*lua.LTable).RawSetInt(1, jobLuaEncoded(t, changed))
		if got, code := f.vm.invoke(t, "Stage", "validate_chunk", ctx, f.run, job, stage, input); got != lua.LNil || code == lua.LNil {
			t.Fatal("changed replay byte accepted", kind)
		}
	}
	return p
}

func stageLuaComplete(t *testing.T, f *stageLuaFixture, max bool) {
	t.Helper()
	outCount, discoverCount, imageCount := 65, 1, 1
	if max {
		outCount, discoverCount, imageCount = 256, 128, 64
	}
	for i, n := range map[int]int{stageExpectedOutlinksIndex: outCount, stageExpectedDiscoveriesIndex: discoverCount, stageExpectedImagesIndex: imageCount} {
		f.meta[i].Value = []byte(strconv.Itoa(n))
	}
	f.r.hash(f.stagePrefix+"meta", f.meta)
	page := stageLuaPage()
	page[9].Value = []byte(f.publication)
	pageFields := Record{page[0], page[3], page[4], page[5], page[6], page[7], page[8], page[9]}
	steps := []struct {
		kind ChunkKind
		ord  uint64
		r    []Record
	}{{ChunkPageFields, 0, []Record{pageFields}}, {ChunkHTML, 0, []Record{{textField("field_name", "html"), textField("field_bytes", "<p>é\x00</p>")}}},
		{ChunkOriginalHTML, 0, []Record{{textField("field_name", "original_html"), textField("field_bytes", "")}}}}
	for start := 0; start < outCount; start += 64 {
		var records []Record
		for i := start; i < start+64 && i < outCount; i++ {
			records = append(records, Record{textField("target_url", fmt.Sprintf("https://example.com/out%03d", i))})
		}
		steps = append(steps, struct {
			kind ChunkKind
			ord  uint64
			r    []Record
		}{ChunkOutlinks, uint64(start / 64), records})
	}
	var discoveries []Record
	for i := 0; i < discoverCount; i++ {
		s := jobLuaSourceValue(t, fmt.Sprintf("https://example.com/discovery%03d", i))
		r, _ := completeDiscoveryRecord(OutputDiscovery{JobID: s.JobID, CanonicalURL: s.CanonicalURL, Depth: s.Depth, ScoreText: s.ScoreText, GroupID: s.GroupID, RateScopeID: s.RateScopeID, Decision: s.Decision})
		discoveries = append(discoveries, r)
	}
	sort.Slice(discoveries, func(i, j int) bool { return string(discoveries[i][0].Value) < string(discoveries[j][0].Value) })
	for start := 0; start < discoverCount; start += 64 {
		end := min(start+64, discoverCount)
		steps = append(steps, struct {
			kind ChunkKind
			ord  uint64
			r    []Record
		}{ChunkDiscoveries, uint64(start / 64), discoveries[start:end]})
	}
	steps = append(steps, struct {
		kind ChunkKind
		ord  uint64
		r    []Record
	}{ChunkAliases, 0, []Record{{textField("url_id", string(f.lease.JobID)), textField("canonical_url", "https://example.com/path"), textField("depth", "0")}}})
	var images []OutputImage
	for i := 0; i < imageCount; i++ {
		images = append(images, OutputImage{NormalizedSourceURL: fmt.Sprintf("https://example.com/image%03d", i), Alt: "é-alt"})
	}
	ir, _ := outputImageRecords(images)
	manifest, _ := NewImageManifestRecord(f.publication, "https://example.com/path", images)
	mr, _ := manifest.Record()
	steps = append(steps, struct {
		kind ChunkKind
		ord  uint64
		r    []Record
	}{ChunkImages, 0, ir}, struct {
		kind ChunkKind
		ord  uint64
		r    []Record
	}{ChunkImageManifest, 0, []Record{mr}})
	for _, step := range steps {
		prepared := f.prepare(t, step.kind, step.ord, step.r, false)
		f.materialize(t, prepared)
		f.prepare(t, step.kind, step.ord, step.r, true)
	}
	// Matching membership is NOT matching a chunk. Keep ordinal-0's original
	// digest but replace its records with another canonical 64-member slice of
	// the same already-stored collection. Every byte exists, but this is NOT its
	// immutable RECORD/SECTION slice. This must reject without bulk Lua SHA.
	for _, kind := range []ChunkKind{ChunkOutlinks, ChunkDiscoveries} {
		if kind == ChunkDiscoveries && !max {
			continue
		}
		var records []Record
		for _, step := range steps {
			if step.kind == kind {
				records = append(records, step.r...)
			}
		}
		if len(records) < 65 {
			t.Fatal("slice regression requires a second chunk")
		}
		op := "CJ2_STAGE_OUTLINKS_BATCH"
		if kind == ChunkDiscoveries {
			op = "CJ2_STAGE_DISCOVERIES_BATCH"
		}
		ctx, view, job := f.open(t, op, "")
		stage, code := f.check(t, view, job, "check_owned")
		if code != lua.LNil {
			t.Fatal(code)
		}
		input := f.input(t, kind, 0, records[:64])
		array := input.RawGetString("records").(*lua.LTable)
		for i, record := range records[1:65] {
			array.RawSetInt(i+1, jobLuaEncoded(t, record))
		}
		if got, code := f.vm.invoke(t, "Stage", "validate_chunk", ctx, f.run, job, stage, input); got != lua.LNil || code != lua.LString("IMMUTABLE_MISMATCH") {
			t.Fatal("wrong stored slice replayed", kind, code)
		}
	}
	if max && string(f.meta[stageKeyCountIndex].Value) != "73" {
		t.Fatal("maximum key count not exact")
	}
	// Fixture's seal is Go-validated; no Stage seal operation is being simulated.
	f.meta[stageSealedIndex].Value, f.meta[stageSealedAtMSIndex].Value = []byte("1"), []byte("500")
	if err := ValidateRecord(SchemaStageMeta, f.meta); err != nil {
		t.Fatal(err)
	}
	f.r.hash(f.stagePrefix+"meta", f.meta)
	_, view, job := f.open(t, "CJ2_SEAL_STAGE", "")
	if _, code := f.check(t, view, job, "check_owned"); code != lua.LNil {
		t.Fatal("sealed full projection", code)
	}
}

func TestStageLuaAllChunksReplayAndMaximumInventory(t *testing.T) {
	t.Parallel()
	for _, maximum := range []bool{false, true} {
		t.Run(fmt.Sprint(maximum), func(t *testing.T) { stageLuaComplete(t, stageLuaFixtureNew(t), maximum) })
	}
}

func TestStageLuaAliasesMaximumAndDiscoverySourceOrder(t *testing.T) {
	t.Parallel()
	f := stageLuaFixtureStarts(t, 6) // enough represented STARTs for five aliases, returning to the source
	f.meta[stageExpectedAliasesIndex].Value = []byte("5")
	f.r.hash(f.stagePrefix+"meta", f.meta)
	var aliases []Record
	for i := 0; i < 5; i++ {
		url := fmt.Sprintf("https://example.com/alias%02d", i)
		if i == 0 {
			url = "https://example.com/path"
		}
		aliases = append(aliases, Record{textField("url_id", string(recordAuthorityURLID(url))), textField("canonical_url", url), textField("depth", "0")})
	}
	sort.Slice(aliases, func(i, j int) bool { return string(aliases[i][0].Value) < string(aliases[j][0].Value) })
	f.materialize(t, f.prepare(t, ChunkAliases, 0, aliases, false))
	f.prepare(t, ChunkAliases, 0, aliases, true)
	ctx, view, job := f.open(t, "CJ2_STAGE_ALIASES_BATCH", "")
	stage, code := f.check(t, view, job, "check_owned")
	if code != lua.LNil {
		t.Fatal(code)
	}
	for _, mutation := range []string{"depth", "duplicate", "six"} {
		input := f.input(t, ChunkAliases, 0, aliases)
		array := input.RawGetString("records").(*lua.LTable)
		switch mutation {
		case "depth":
			r := cloneRecord(aliases[0])
			r[2].Value = []byte("1")
			array.RawSetInt(1, jobLuaEncoded(t, r))
		case "duplicate":
			array.RawSetInt(2, array.RawGetInt(1))
		case "six":
			array.RawSetInt(6, array.RawGetInt(1))
			input.RawGetString("v").(*lua.LTable).RawSetString("record_count", lua.LString("6"))
		}
		if got, _ := f.vm.invoke(t, "Stage", "validate_chunk", ctx, f.run, job, stage, input); got != lua.LNil {
			t.Fatal("invalid alias collection", mutation)
		}
	}
	source := jobLuaSourceValue(t, "https://example.com/discovered")
	source.ScoreText = "0.1"
	discovery, _ := completeDiscoveryRecord(OutputDiscovery{JobID: source.JobID, CanonicalURL: source.CanonicalURL, Depth: source.Depth, ScoreText: source.ScoreText, GroupID: source.GroupID, RateScopeID: source.RateScopeID, Decision: source.Decision})
	p := f.prepare(t, ChunkDiscoveries, 0, []Record{discovery}, false)
	r := p.RawGetString("records").(*lua.LTable).RawGetInt(1).(*lua.LTable)
	jobLuaAssertRecord(t, r, discovery)
	want, _ := completeSourceJobRecord(source)
	jobLuaAssertRecord(t, r.RawGetString("source"), want)
	ctx, view, job = f.open(t, "CJ2_STAGE_DISCOVERIES_BATCH", "")
	stage, _ = f.check(t, view, job, "check_owned")
	input := f.input(t, ChunkDiscoveries, 0, []Record{discovery})
	input.RawGetString("records").(*lua.LTable).RawSetInt(1, jobLuaEncoded(t, want))
	if got, _ := f.vm.invoke(t, "Stage", "validate_chunk", ctx, f.run, job, stage, input); got != lua.LNil {
		t.Fatal("source field order silently accepted as discovery chunk")
	}
}

func TestStageLuaAbortedTerminalSlotAndDeletionOnlyNoops(t *testing.T) {
	t.Parallel()
	f := stageLuaFixtureNew(t)
	id, _ := DeriveAbortStageTransitionID(AbortStageTransitionInput{Lease: f.lease, CommitID: f.commit})
	f.job[jobActiveStageCommitIDIndex].Value = []byte("")
	f.job[jobLastTransitionIDIndex].Value, f.job[jobLastTransitionStatusIndex].Value = []byte(id), []byte(StatusStageAborted)
	f.r.hash(f.jobKey, f.job)
	delete(f.r.hashes, f.stagePrefix+"meta")
	delete(f.r.lists, f.stagePrefix+"keys")
	delete(f.r.zsets, StageExpiryKey)
	f.r.hashes[StageSlotsKey][string(f.commit)] = strings.TrimSuffix(f.r.hashes[StageSlotsKey][string(f.commit)], ":0") + ":2"
	_, view, job := f.open(t, "CJ2_ABORT_STAGE", "")
	if _, code := f.check(t, view, job, "check_aborted"); code != lua.LNil {
		t.Fatal(code)
	}
	if got, _ := f.check(t, view, job, "check_owned"); got != lua.LNil {
		t.Fatal("terminal slot became stage authority")
	}
	if got, _ := f.check(t, view, job, "check_residue"); got != lua.LNil {
		t.Fatal("terminal reserve became cleanup authority")
	}
	for _, count := range []string{"0", "1", "74", "02"} {
		f.r.hashes[StageSlotsKey][string(f.commit)] = "50000000:" + string(f.lease.RunID) + ":" + string(f.lease.JobID) + ":1:" + count
		_, v, j := f.open(t, "CJ2_ABORT_STAGE", "")
		if got, _ := f.check(t, v, j, "check_aborted"); got != lua.LNil {
			t.Fatal("bad tombstone count accepted", count)
		}
	}
	delete(f.r.hashes, StageSlotsKey)
	_, view, job = f.open(t, "CJ2_CLEAN_STAGE", "")
	got, code := f.check(t, view, job, "check_residue")
	if code != lua.LNil || got.(*lua.LTable).RawGetString("kind") != lua.LString("absent") || got.(*lua.LTable).RawGetString("key_count") != lua.LNumber(0) {
		t.Fatal("explicit deletion noop", code)
	}
	f.r.zsets[StageExpiryKey] = map[string]string{string(f.commit): "500"}
	_, view, job = f.open(t, "CJ2_CLEAN_STAGE", "")
	got, code = f.check(t, view, job, "check_residue")
	if code != lua.LNil || got.(*lua.LTable).RawGetString("kind") != lua.LString("expired_residue") {
		t.Fatal("expired absent bundle", code)
	}
}

func TestStageLuaRecoveredResidueNeverRebindsBG(t *testing.T) {
	t.Parallel()
	f := stageLuaFixtureNew(t)
	delete(f.r.hashes, StageSlotsKey)
	f.r.hashes[f.stagePrefix+"meta"]["abandoned"] = "1"
	_, view, job := f.open(t, "CJ2_CLEAN_STAGE", "")
	// A historical residue must not consult supplied current job B/G, nor make
	// them match by rewriting either side. It grants only closed-key deletion.
	job.RawGetString("v").(*lua.LTable).RawSetString("request_starts", lua.LString("9"))
	got, code := f.check(t, view, job, "check_residue")
	if code != lua.LNil || got.(*lua.LTable).RawGetString("kind") != lua.LString("recovered_residue") || got.(*lua.LTable).RawGetString("deletion_only") != lua.LTrue {
		t.Fatal(code)
	}
	if got.(*lua.LTable).RawGetString("key_count") != lua.LNumber(2) {
		t.Fatal("closed deletion count")
	}
}

func TestStageLuaResidueZeroTTLExactBoundaryOnly(t *testing.T) {
	t.Parallel()
	const due = uint64(900500)
	for _, tc := range []struct {
		name      string
		now       uint64
		ttl       int64
		ownedSlot bool
		badList   bool
		valid     bool
	}{
		{"future-positive", due - 1, 1, false, false, true},
		{"future-zero", due - 1, 0, false, false, false},
		{"due-zero", due, 0, false, false, true},
		{"due-extended", due, 1, false, false, false},
		{"due-persistent", due, -1, false, false, false},
		{"past-zero-is-not-the-recorded-expiry", due + 1, 0, false, false, false},
		{"due-owned-slot", due, 0, true, false, false},
		{"due-malicious-inventory", due, 0, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := stageLuaFixtureNew(t)
			f.r.hashes[f.stagePrefix+"meta"]["abandoned"] = "1"
			if !tc.ownedSlot {
				delete(f.r.hashes, StageSlotsKey)
			}
			f.r.nowMS = tc.now
			f.r.ttls[f.stagePrefix+"meta"], f.r.ttls[f.stagePrefix+"keys"] = tc.ttl, tc.ttl
			if tc.badList {
				f.r.lists[f.stagePrefix+"keys"][0] = f.stagePrefix + "../outside"
			}
			ctx, view, job := f.open(t, "CJ2_CLEAN_STAGE", "")
			result, code := f.check(t, view, job, "check_residue")
			if !tc.valid {
				if result != lua.LNil || code != lua.LString("STAGE_INVALID") {
					t.Fatal("invalid materialized residue TTL/inventory accepted", result, code)
				}
				return
			}
			if code != lua.LNil || result.(*lua.LTable).RawGetString("kind") != lua.LString("recovered_residue") ||
				result.(*lua.LTable).RawGetString("key_count") != lua.LNumber(2) {
				t.Fatal("valid residue boundary rejected", result, code)
			}
			if _, code := f.vm.invoke(t, "Stage", "slot_proof", result, ctx); code != lua.LString("INVALID_STATE") {
				t.Fatal("cleanup boundary became allocation authority")
			}
		})
	}
	// The same zero TTL is never an owned-stage receipt, even while the lease
	// and original stage expiry are still in the future. No live predicate changes.
	f := stageLuaFixtureNew(t)
	f.r.ttls[f.stagePrefix+"meta"], f.r.ttls[f.stagePrefix+"keys"] = 0, 0
	_, view, job := f.open(t, "CJ2_STAGE_PAGE_FIELDS", "")
	if result, code := f.check(t, view, job, "check_owned"); result != lua.LNil || code != lua.LString("STAGE_INVALID") {
		t.Fatal("owned-stage positive TTL guard relaxed", result, code)
	}
}

func TestStageLuaBeginPreparationNoAdmission(t *testing.T) {
	t.Parallel()
	f := stageLuaFixtureNew(t)
	for _, index := range []int{jobActiveStageCommitIDIndex, jobLastStageCommitIDIndex} {
		f.job[index].Value = []byte("")
	}
	f.job[jobLastStageFenceIndex].Value = []byte("0")
	f.r.hash(f.jobKey, f.job)
	delete(f.r.hashes, f.stagePrefix+"meta")
	delete(f.r.lists, f.stagePrefix+"keys")
	delete(f.r.hashes, StageSlotsKey)
	delete(f.r.zsets, StageExpiryKey)
	ctx, _, job := f.open(t, "CJ2_BEGIN_STAGE", "")
	v := f.leaseValue()
	for _, field := range f.meta {
		// This input deliberately has unused metadata fields too. The selected
		// BEGIN projection must derive its clock/token/progress, not copy them.
		v.RawSetString(field.Name, lua.LString(field.Value))
	}
	v.RawSetString("created_at_ms", lua.LString("1"))
	v.RawSetString("token_digest", lua.LString(strings.Repeat("f", 64)))
	input := f.vm.state.NewTable()
	input.RawSetString("v", v)
	before := len(f.r.trace)
	got, code := f.vm.invoke(t, "Stage", "begin_record", ctx, f.run, job, f.leaseValue(), input)
	if code != lua.LNil {
		t.Fatal(code)
	}
	jobLuaAssertRecord(t, got.(*lua.LTable).RawGetString("meta"), f.meta)
	beginHandle := got
	beginProof, proofCode := f.vm.invoke(t, "Stage", "begin_proof", beginHandle, ctx)
	if proofCode != lua.LNil || beginProof.(*lua.LTable).RawGetString("kind") != lua.LString("begin") ||
		beginProof.(*lua.LTable).RawGetString("remaining") != lua.LNumber(StageMemoryReservationBytes) {
		t.Fatal("BEGIN private proof", proofCode)
	}
	// Neither editing a returned projection nor supplying a numeric growth can
	// forge the private begin authority used by the separately owned core solver.
	got.(*lua.LTable).RawGetString("meta").(*lua.LTable).RawGetString("v").(*lua.LTable).RawSetString("commit_id", lua.LString(strings.Repeat("f", 64)))
	beginProof, proofCode = f.vm.invoke(t, "Stage", "slot_proof", beginHandle, ctx)
	if proofCode != lua.LNil || beginProof.(*lua.LTable).RawGetString("commit_id") != lua.LString(f.commit) {
		t.Fatal("edited BEGIN projection changed private proof")
	}
	if _, code := f.vm.invoke(t, "Stage", "begin_proof", lua.LNumber(65536), ctx); code != lua.LString("INVALID_STATE") {
		t.Fatal("numeric growth accepted as BEGIN proof")
	}
	if before != len(f.r.trace) || got.(*lua.LTable).RawGetString("growth") != lua.LNil {
		t.Fatal("constructor attempted memory admission")
	}
	for _, name := range []string{"request_starts_baseline", "request_starts_generation", "commit_id", "publication_id", "output_digest", "owner_id", "expected_page_fields", "expected_aliases", "expected_images"} {
		original := v.RawGetString(name)
		v.RawSetString(name, lua.LString("0"))
		if got, _ := f.vm.invoke(t, "Stage", "begin_record", ctx, f.run, job, f.leaseValue(), input); got != lua.LNil && name != "request_starts_baseline" && name != "expected_images" {
			t.Fatal("bad begin field accepted", name)
		}
		v.RawSetString(name, original)
	}
	// MAX exact arithmetic is tested before building expiry/key arguments. A
	// source-owned opened context is required even at overflow (no system clock).
	ctx.RawSetString("now_ms", lua.LNumber(MaxExactInteger))
	ctx.RawSetString("now_text", lua.LString(canonicalDecimal(MaxExactInteger)))
	if got, _ := f.vm.invoke(t, "Stage", "begin_record", ctx, f.run, job, f.leaseValue(), input); got != lua.LNil {
		t.Fatal("out-of-lifetime begin accepted")
	}
}

func TestStageLuaCommittedResidueAndRetainedReceipt(t *testing.T) {
	t.Parallel()
	f := stageLuaFixtureNew(t)
	stageLuaComplete(t, f, false)
	for _, index := range []int{jobLeaseOwnerIndex, jobLeaseTokenIndex, jobActiveStageCommitIDIndex} {
		f.job[index].Value = []byte("")
	}
	for _, index := range []int{jobLeaseStartedAtMSIndex, jobLeaseExpiresAtMSIndex, jobLeaseDeliveryStartedIndex} {
		f.job[index].Value = []byte("0")
	}
	pageKey, _ := PageDataKey(f.publication, "https://example.com/path")
	for index, value := range map[int]string{jobStateIndex: "completed", jobLastReasonIndex: "published", jobCompletedAtMSIndex: "550", jobUpdatedAtMSIndex: "550",
		jobOutputDigestIndex: string(f.meta[stageOutputDigestIndex].Value), jobPublicationIDIndex: string(f.publication), jobCommitIDIndex: string(f.commit), jobPublishedPageKeyIndex: pageKey} {
		f.job[index].Value = []byte(value)
	}
	if err := ValidateRecord(SchemaJob, f.job); err != nil {
		t.Fatal(err)
	}
	f.r.hash(f.jobKey, f.job)
	f.indices()
	delete(f.r.hashes, StageSlotsKey)
	for _, name := range []string{"page", "outlinks", "image_manifest", "image:0"} {
		delete(f.r.hashes, f.stagePrefix+name)
		delete(f.r.sets, f.stagePrefix+name)
	}
	f.r.zsets[StageExpiryKey][string(f.commit)] = "60550"
	f.r.nowMS = 600
	for _, key := range f.stageKeys {
		f.r.ttls[key] = 59950
	}
	_, view, job := f.open(t, "CJ2_COMMIT", "")
	got, code := f.check(t, view, job, "check_residue")
	if code != lua.LNil || got.(*lua.LTable).RawGetString("kind") != lua.LString("committed_residue") || got.(*lua.LTable).RawGetString("key_count") != lua.LNumber(6) {
		t.Fatal("committed residue", code)
	}
	got, code = f.check(t, view, job, "check_committed")
	if code != lua.LNil || got.(*lua.LTable).RawGetString("publication_id") != lua.LString(f.publication) {
		t.Fatal("retained receipt", code)
	}
	// Commit formula intentionally excludes owner. Cleared lease fields cannot
	// establish or reject ALREADY_COMMITTED; token and fence remain identity inputs.
	lease := f.leaseValue()
	lease.RawSetString("owner_id", lua.LString(strings.Repeat("f", 32)))
	if _, code := f.vm.invoke(t, "Stage", "check_committed", view, f.run, job, lease, lua.LString(f.commit)); code != lua.LNil {
		t.Fatal("receipt compared cleared owner", code)
	}
	lease.RawSetString("lease_token", lua.LString(strings.Repeat("f", 64)))
	if got, code := f.vm.invoke(t, "Stage", "check_committed", view, f.run, job, lease, lua.LString(f.commit)); got != lua.LNil || code != lua.LString("LEASE_LOST") {
		t.Fatal("changed token accepted", code)
	}
	// Original metadata expiry is immutable; retained residue TTL is the shortened
	// exact min(completed+60s, original). An extension is not a renewal.
	f.r.ttls[f.stagePrefix+"meta"]++
	_, view, job = f.open(t, "CJ2_COMMIT", "")
	if got, _ := f.check(t, view, job, "check_residue"); got != lua.LNil {
		t.Fatal("residue TTL extension accepted")
	}
	// Downstream output and residual stage keys may already be gone. The retained
	// receipt never tries to recreate them or requires their continued existence.
	for _, key := range f.stageKeys {
		delete(f.r.hashes, key)
		delete(f.r.sets, key)
		delete(f.r.zsets, key)
		delete(f.r.lists, key)
	}
	delete(f.r.zsets, StageExpiryKey)
	_, view, job = f.open(t, "CJ2_COMMIT", f.stagePrefix+"page")
	if _, code := f.check(t, view, job, "check_committed"); code != lua.LNil {
		t.Fatal("retained receipt required deleted stage", code)
	}
}

func TestStageLuaChunkStructuralAndCrossRecordNegatives(t *testing.T) {
	t.Parallel()
	f := stageLuaFixtureNew(t)
	page := stageLuaPage()
	page[9].Value = []byte(f.publication)
	pageFields := Record{page[0], page[3], page[4], page[5], page[6], page[7], page[8], page[9]}
	ctx, view, job := f.open(t, "CJ2_STAGE_PAGE_FIELDS", "")
	stage, code := f.check(t, view, job, "check_owned")
	if code != lua.LNil {
		t.Fatal(code)
	}
	for i, field := range pageFields {
		for _, value := range []string{"", "\xff", "0", "https://example.com/other", "Fri, 02 Jan 1970 00:00:00 UTC", strings.Repeat("a", 64)} {
			input := f.input(t, ChunkPageFields, 0, []Record{pageFields})
			r := cloneRecord(pageFields)
			r[i].Value = []byte(value)
			input.RawGetString("records").(*lua.LTable).RawSetInt(1, jobLuaEncoded(t, r))
			got, _ := f.vm.invoke(t, "Stage", "validate_chunk", ctx, f.run, job, stage, input)
			want := validatePageFieldsChunk(StageChunk{kind: ChunkPageFields, records: []Record{r}})
			contextual := (field.Name == "normalized_url" && value != string(page[0].Value)) || (field.Name == "last_crawled" && value != string(page[5].Value)) || (field.Name == "publication_id" && value != string(f.publication))
			if want != nil || contextual {
				if got != lua.LNil {
					t.Fatal("bad metadata admitted", field.Name)
				}
			}
		}
	}
	for _, field := range []string{"chunk_kind", "chunk_ordinal", "chunk_digest", "record_count", "fence", "commit_id", "run_id", "job_id", "owner_id", "lease_token"} {
		input := f.input(t, ChunkPageFields, 0, []Record{pageFields})
		input.RawGetString("v").(*lua.LTable).RawSetString(field, lua.LString("bogus"))
		if got, _ := f.vm.invoke(t, "Stage", "validate_chunk", ctx, f.run, job, stage, input); got != lua.LNil {
			t.Fatal("invalid chunk scalar accepted", field)
		}
	}
	// An exact Go-valid second slice cannot skip the first. Expected first slice
	// is exactly 64 records, not an arbitrary small incremental batch.
	ctx, view, job = f.open(t, "CJ2_STAGE_OUTLINKS_BATCH", "")
	stage, _ = f.check(t, view, job, "check_owned")
	for _, ordinal := range []uint64{0, 1, 3} {
		input := f.input(t, ChunkOutlinks, ordinal, []Record{{textField("target_url", "https://example.com/x")}})
		if got, _ := f.vm.invoke(t, "Stage", "validate_chunk", ctx, f.run, job, stage, input); got != lua.LNil {
			t.Fatal("short/skipped/out-of-range slice accepted", ordinal)
		}
	}
	ctx, view, job = f.open(t, "CJ2_STAGE_IMAGE_MANIFEST", "")
	stage, _ = f.check(t, view, job, "check_owned")
	manifest, _ := NewImageManifestRecord(f.publication, "https://example.com/path", []OutputImage{{NormalizedSourceURL: "https://example.com/i"}})
	mr, _ := manifest.Record()
	input := f.input(t, ChunkImageManifest, 0, []Record{mr})
	if got, _ := f.vm.invoke(t, "Stage", "validate_chunk", ctx, f.run, job, stage, input); got != lua.LNil {
		t.Fatal("manifest before image payload accepted")
	}
	// A fabricated table cannot replace the internally retained ownership proof.
	if got, _ := f.vm.invoke(t, "Stage", "validate_chunk", ctx, f.run, job, f.vm.state.NewTable(), input); got != lua.LNil {
		t.Fatal("fabricated ownership handle accepted")
	}
}

func TestStageLuaControlRemainingIsNotCallerGrowth(t *testing.T) {
	t.Parallel()
	f := stageLuaFixtureNew(t)
	for _, remaining := range []string{"0", "1", "9999999", "10000000", "50331648", "50331649", "-1", "00", "9007199254740991"} {
		f.r.hashes[StageSlotsKey][string(f.commit)] = remaining + ":" + string(f.lease.RunID) + ":" + string(f.lease.JobID) + ":1:0"
		ctx, view, job := f.open(t, "CJ2_STAGE_PAGE_FIELDS", "")
		stage, code := f.check(t, view, job, "check_owned")
		n, err := ParseUnsignedDecimal(remaining)
		value, _ := n.Uint64()
		if err != nil || value > StageMemoryReservationBytes {
			if stage != lua.LNil {
				t.Fatal("invalid slot remaining accepted", remaining)
			}
			continue
		}
		if code != lua.LNil {
			t.Fatal("corrupt-low R must remain exact for the parent's finite solver", code)
		}
		p, code := f.vm.invoke(t, "Stage", "slot_proof", stage, ctx)
		if code != lua.LNil || p.(*lua.LTable).RawGetString("remaining") != lua.LNumber(value) || p.(*lua.LTable).RawGetString("commit_id") != lua.LString(f.commit) {
			t.Fatal("slot proof overcharged or lost owner", code)
		}
	}
}

func TestStageLuaSealedPublicHandleAndContextCannotAuthorizeWrites(t *testing.T) {
	t.Parallel()
	f := stageLuaFixtureNew(t)
	stageLuaComplete(t, f, false)
	ctx, view, job := f.open(t, "CJ2_STAGE_PAGE_FIELDS", "")
	stage, code := f.check(t, view, job, "check_owned")
	if code != lua.LNil {
		t.Fatal(code)
	}
	stage.(*lua.LTable).RawGetString("v").(*lua.LTable).RawSetString("sealed", lua.LString("0"))
	stage.(*lua.LTable).RawGetString("bundle").(*lua.LTable).RawGetString("final_keys").(*lua.LTable).RawSetString("page", lua.LString("arbitrary:victim"))
	publication, proofCode := f.vm.invoke(t, "Stage", "publication_proof", stage, ctx)
	if proofCode != lua.LNil || publication.(*lua.LTable).RawGetString("page_url") != lua.LString("https://example.com/path") ||
		publication.(*lua.LTable).RawGetString("final_keys") != lua.LNil {
		t.Fatal("publication proof followed caller-selected path")
	}
	page := stageLuaPage()
	page[9].Value = []byte(f.publication)
	input := f.input(t, ChunkPageFields, 0, []Record{{page[0], page[3], page[4], page[5], page[6], page[7], page[8], page[9]}})
	if got, _ := f.vm.invoke(t, "Stage", "validate_chunk", ctx, f.run, job, stage, input); got != lua.LNil {
		t.Fatal("edited public sealed marker authorized data writes")
	}
	if _, code := f.vm.invoke(t, "Context", "seal", ctx); code != lua.LNil {
		t.Fatal(code)
	}
	if got, _ := f.vm.invoke(t, "Stage", "slot_proof", stage, ctx); got != lua.LNil {
		t.Fatal("post-seal proof accepted")
	}
}

func TestStageLuaZeroCollectionsAndBlobSHAException(t *testing.T) {
	t.Parallel()
	f := stageLuaFixtureNew(t)
	for _, index := range []int{stageExpectedOutlinksIndex, stageExpectedDiscoveriesIndex, stageExpectedImagesIndex} {
		f.meta[index].Value = []byte("0")
	}
	f.r.hash(f.stagePrefix+"meta", f.meta)
	m, _ := NewImageManifestRecord(f.publication, "https://example.com/path", nil)
	mr, _ := m.Record()
	f.materialize(t, f.prepare(t, ChunkImageManifest, 0, []Record{mr}, false))
	f.prepare(t, ChunkImageManifest, 0, []Record{mr}, true)
	// The guard delegates to the real Lua SHA primitive. It fails the test if
	// any validator attempts bulk hashing; it does not substitute a Go digest.
	original := f.vm.module.RawGetString("sha256")
	f.vm.module.RawSetString("sha256", f.vm.state.NewFunction(func(l *lua.LState) int {
		if len(l.CheckString(1)) > 16384 {
			l.RaiseError("bulk Lua SHA forbidden")
			return 0
		}
		if err := l.CallByParam(lua.P{Fn: original, NRet: 2, Protect: true}, l.Get(1)); err != nil {
			l.RaiseError("SHA delegation: %s", err)
			return 0
		}
		return 2
	}))
	for _, kind := range []ChunkKind{ChunkHTML, ChunkOriginalHTML} {
		blob := Record{textField("field_name", string(kind)), textField("field_bytes", strings.Repeat("é", MaxPageBlobBytes/2))}
		f.materialize(t, f.prepare(t, kind, 0, []Record{blob}, false))
		f.prepare(t, kind, 0, []Record{blob}, true)
	}
	ctx, view, job := f.open(t, "CJ2_STAGE_PAGE_BLOB", "")
	stage, code := f.check(t, view, job, "check_owned")
	if code != lua.LNil {
		t.Fatal(code)
	}
	input := f.input(t, ChunkHTML, 0, []Record{{textField("field_name", "html"), textField("field_bytes", "")}})
	input.RawGetString("records").(*lua.LTable).RawSetInt(1, jobLuaEncoded(t, Record{textField("field_name", "html"), textField("field_bytes", strings.Repeat("x", MaxPageBlobBytes+1))}))
	if got, _ := f.vm.invoke(t, "Stage", "validate_chunk", ctx, f.run, job, stage, input); got != lua.LNil {
		t.Fatal("oversized blob accepted")
	}
}
