package crawljobsv2

import (
	"bytes"
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

var stageOpsInventory = []OperationName{OperationBeginStage, OperationStagePageFields, OperationStagePageBlob,
	OperationStageOutlinksBatch, OperationStageDiscoveriesBatch, OperationStageAliasesBatch, OperationStageImagesBatch,
	OperationStageImageManifest, OperationAbortStage, OperationSealStage, OperationCommit}

func stageOpsCore(t *testing.T) string {
	t.Helper()
	if runtime.Version() != "go1.25.13" {
		t.Fatal("Stage operation oracles require Go 1.25.13")
	}
	return recordsLuaCore(t) +
		"local actualStageSHA=P.sha256\nP.sha256=function(s) if #s>1048576 then error('bulk Lua SHA is forbidden') end return actualStageSHA(s) end\n" +
		"CJ.Request=(function()\n" + string(primitiveLuaRead(t, "lua_src/ledger_request.lua")) + "\nend)()\n" +
		"CJ.Rate=CJ.Request.Rate\n" +
		"CJ.StageOutput=(function()\n" + string(primitiveLuaRead(t, "lua_src/stage_output.lua")) + "\nend)()\n" +
		"CJ.Stage=(function()\n" + string(primitiveLuaRead(t, "lua_src/ledger_stage.lua")) + "\nend)()\n"
}

func stageOpsSource(t *testing.T, op OperationName) string {
	t.Helper()
	return stageOpsCore(t) + string(primitiveLuaRead(t, "lua_src/ops/"+strings.ToLower(string(op))+".lua"))
}

// Only Redis command semantics. The REAL core builds permissions, private read
// receipts, allocation estimates, exact slot settlement, sealed plans and replies.
// No Go callback stands in for a Stage operation or a memory admission policy.
type stageOpsRedis struct{ *sharedLuaRedis }

func stageOpsWrite(name string) bool {
	return sharedLuaIsWrite(name) || name == "LPUSH" || name == "RPUSH" || name == "PEXPIREAT"
}

func (r *stageOpsRedis) command(l *lua.LState, acl bool) int {
	name := l.CheckString(1)
	if name != "LPUSH" && name != "RPUSH" && name != "PEXPIREAT" {
		return r.sharedLuaRedis.command(l, acl)
	}
	args := make([]string, l.GetTop()-1)
	for i := range args {
		v, ok := l.Get(i + 2).(lua.LString)
		if !ok {
			l.RaiseError("stage facade expected bulk arguments")
			return 0
		}
		args[i] = string(v)
	}
	r.trace = append(r.trace, bootLuaCommand{name: name, args: args, acl: acl})
	if acl {
		r.aclCount++
		r.prebuilt = sharedLuaExecutionReply(l)
		if r.prebuilt == nil {
			l.RaiseError("ACL check before prebuilt execution/reply")
			return 0
		}
		l.Push(lua.LBool(r.denyAt != r.aclCount))
		return 1
	}
	r.attempts++
	if r.failAt == r.attempts && !r.failAfter {
		l.RaiseError(bootLuaWriteErr)
		return 0
	}
	if len(args) < 2 {
		l.RaiseError("stage facade mutation arity")
		return 0
	}
	key := args[0]
	entry, exists := r.data[key]
	if exists && entry.expireAt > 0 && uint64(entry.expireAt) <= r.now {
		r.removeKey(key)
		entry, exists = bootLuaEntry{}, false
	}
	var result int
	if name == "PEXPIREAT" {
		at, err := strconv.ParseUint(args[1], 10, 64)
		if len(args) != 2 || err != nil || at > MaxExactInteger {
			l.RaiseError("invalid PEXPIREAT")
			return 0
		}
		if exists {
			result = 1
			if at <= r.now {
				r.removeKey(key)
			} else {
				entry.expireAt = int64(at)
				r.data[key] = entry
			}
		}
	} else {
		if exists && entry.kind != "list" {
			l.RaiseError("WRONGTYPE")
			return 0
		}
		if !exists {
			entry = bootLuaEntry{kind: "list", expireAt: -1}
		}
		values := r.lists[key]
		for _, value := range args[1:] {
			if name == "LPUSH" {
				values = append([]string{value}, values...)
			} else {
				values = append(values, value)
			}
		}
		r.data[key], r.lists[key] = entry, values
		result = len(values)
	}
	r.writes++
	if r.failAt == r.attempts && r.failAfter {
		l.RaiseError(bootLuaWriteErr)
		return 0
	}
	l.Push(lua.LNumber(result))
	return 1
}

func stageOpsRun(t *testing.T, r *stageOpsRedis, source string, keys, args []string) bootLuaResult {
	t.Helper()
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()
	lua.OpenBase(l)
	lua.OpenMath(l)
	lua.OpenString(l)
	lua.OpenTable(l)
	l.SetTop(0)
	for _, name := range []string{"dofile", "loadfile", "load", "loadstring", "collectgarbage", "print"} {
		l.SetGlobal(name, lua.LNil)
	}
	l.SetGlobal("bit", primitiveLuaBitOp(l))
	l.SetContext(luaTestContext(t))
	r.trace, r.aclCount, r.attempts, r.prebuilt, r.returnedPrebuilt = nil, 0, 0, nil, false
	redis := l.NewTable()
	l.SetFuncs(redis, map[string]lua.LGFunction{
		"call":          func(l *lua.LState) int { return r.command(l, false) },
		"acl_check_cmd": func(l *lua.LState) int { return r.command(l, true) },
		"error_reply": func(l *lua.LState) int {
			v := l.NewTable()
			v.RawSetString("err", lua.LString(l.CheckString(1)))
			l.Push(v)
			return 1
		},
	})
	l.SetGlobal("redis", redis)
	l.SetGlobal("KEYS", bootLuaStrings(l, keys))
	l.SetGlobal("ARGV", bootLuaStrings(l, args))
	fn, err := l.Load(strings.NewReader(source), "@stage-operations.lua")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}); err != nil {
		return bootLuaResult{runtimeErr: err}
	}
	r.returnedPrebuilt = r.prebuilt != nil && r.prebuilt == l.Get(-1)
	return bootLuaResult{raw: bootLuaRESP(l.Get(-1))}
}

type stageOpsFixture struct {
	r           *stageOpsRedis
	a           gateArtifacts
	context     OutputContext
	source      SourceJob
	lease       LeaseIdentity
	identity    CommitIdentity
	output      CrawlOutput
	digest      Digest
	commit      Digest
	publication Digest
	chunks      map[string][]StageChunk
	digests     map[string][]string
	runKey      string
	jobKey      string
	prefix      string
}

func stageOpsFixtureNew(t *testing.T, outlinks, discoveries, images int) *stageOpsFixture {
	t.Helper()
	shared, a, _ := runLuaFixture(t, false)
	r := &stageOpsRedis{sharedLuaRedis: shared}
	source := jobLuaSourceValue(t, "https://example.com/path")
	group := jobLuaGroup(source)
	lease := LeaseIdentity{RunID: RunID(runLuaID), JobID: source.JobID, OwnerID: OwnerID(strings.Repeat("3", 32)), Token: LeaseToken(strings.Repeat("4", 64)), Fence: 1}
	policy := Digest(strings.Repeat("a", 64))
	renderBytes := testDenyAllRenderPolicyArtifact()
	renderDigest := plainSHA256(renderBytes)
	groupDigest, _ := DerivePolicyGroupMapDigest([]PolicyGroup{group})
	sourceDigest, _ := DeriveSourceDigest([]SourceJob{source})
	run := recordAuthorityRunRecord(t, "active")
	values := map[string]string{
		"contract_sha256": string(a.contract), "source_sha256": string(sourceDigest), "expected_seed_count": "1", "policy_group_count": "1", "policy_group_map_sha256": string(groupDigest),
		"crawl_policy_sha256": string(policy), "render_policy_sha256": string(renderDigest), "authorization_expires_at_ms": canonicalDecimal(r.now + 3600000),
		"job_count": "1", "open_job_count": "1", "request_starts": "1", "reservation_creations_total": "1", "claims_total": "1", "completed_total": "0", "dead_total": "0", "output_commits_total": "0",
		"load_revision": "1", "audit_revision": "1", "audit_count": "1", "audit_cursor": string(source.JobID), "audit_complete": "1",
		"created_at_ms": canonicalDecimal(r.now - 10000), "sealed_at_ms": canonicalDecimal(r.now - 9000), "activated_at_ms": canonicalDecimal(r.now - 8000),
		"last_activity_at_ms": canonicalDecimal(r.now - 1000), "last_execution_at_ms": canonicalDecimal(r.now - 1000), "last_request_started_at_ms": canonicalDecimal(r.now - 1000), "last_terminal_transition_at_ms": "0",
	}
	for i := range run {
		if v, ok := values[run[i].Name]; ok {
			run[i].Value = []byte(v)
		}
	}
	if err := ValidateRecord(SchemaRun, run); err != nil {
		t.Fatal(err)
	}
	authority, err := newTestTransportAuthority().parseRunPolicyAuthority(lease.RunID, run, []PolicyGroup{group})
	if err != nil {
		t.Fatal(err)
	}
	render, err := NewRenderPolicyAuthorization(authority, renderBytes)
	if err != nil {
		t.Fatal(err)
	}
	target := RequestTarget{URLID: source.JobID, CanonicalURL: source.CanonicalURL}
	event := newTestSuccessfulStartEvent(t, authority, source, lease, policy, RequestDocument, target, 1, r.now-1000, 1, 1, 1)
	transcript, err := NewDocumentTranscript(authority, source, event)
	if err != nil {
		t.Fatal(err)
	}
	targetDigest, _ := DeriveTargetDigest(target)
	witness, err := newTestTransportAuthority().parseFinalDocumentWitness(lease, []string{canonicalDecimal(r.now - 1000), "1", string(source.JobID), source.CanonicalURL, string(targetDigest), "1", canonicalDecimal(r.now - 1000), "0", "leased", string(lease.OwnerID), string(lease.Token), "1", ""})
	if err != nil {
		t.Fatal(err)
	}
	outputContext, err := NewOutputContext(authority, source, transcript, witness, render)
	if err != nil {
		t.Fatal(err)
	}
	output := CrawlOutput{Page: OutputPage{NormalizedURL: source.CanonicalURL, HTML: []byte("<html>é\x00</html>"), ContentType: "text/html; charset=utf-8", StatusCode: 200}}
	for i := 0; i < outlinks; i++ {
		output.Outlinks = append(output.Outlinks, fmt.Sprintf("https://example.com/out/%03d", i))
	}
	for i := 0; i < discoveries; i++ {
		s := recordsLuaJob(t, group, fmt.Sprintf("https://example.com/new/%03d", i), 1, "0.1")
		output.Discoveries = append(output.Discoveries, OutputDiscovery{JobID: s.JobID, CanonicalURL: s.CanonicalURL, ScoreText: s.ScoreText, Depth: s.Depth, GroupID: s.GroupID, RateScopeID: s.RateScopeID, Decision: s.Decision})
	}
	for i := 0; i < images; i++ {
		output.Images = append(output.Images, OutputImage{NormalizedSourceURL: fmt.Sprintf("https://example.com/img/%03d.png", i), Alt: "é-image"})
	}
	f := &stageOpsFixture{r: r, a: a, context: outputContext, source: source, lease: lease, output: output, runKey: runLuaKey("")}
	f.rebuildOutput(t)
	f.jobKey, _ = RunJobKey(lease.RunID, lease.JobID)
	job := recordAuthorityJobRecord(t, "leased")
	srcRecord, _ := completeSourceJobRecord(source)
	jobLuaApplySource(job, srcRecord)
	for index, value := range map[int]string{
		jobCreatedAtMSIndex: canonicalDecimal(r.now - 7000), jobUpdatedAtMSIndex: canonicalDecimal(r.now - 1000), jobLeaseStartedAtMSIndex: canonicalDecimal(r.now - 2000), jobLeaseExpiresAtMSIndex: canonicalDecimal(r.now + 60000),
		jobLastRequestStartedAtMSIndex: canonicalDecimal(r.now - 1000), jobLastDocumentRequestStartedAtMSIndex: canonicalDecimal(r.now - 1000), jobLastDocumentRequestFenceIndex: "1", jobLastDocumentTargetURLIDIndex: string(source.JobID),
		jobLastDocumentTargetURLIndex: source.CanonicalURL, jobLastDocumentTargetDigestIndex: string(targetDigest), jobActiveReservationIDIndex: "",
	} {
		job[index].Value = []byte(value)
	}
	if err := ValidateRecord(SchemaJob, job); err != nil {
		t.Fatal(err)
	}
	r.setHash(f.runKey, run)
	r.setHash(f.jobKey, job)
	for name, value := range map[string]string{"group_limits": "10", "group_rate_scope_ids": string(group.RateScopeID), "group_scope_ids": string(group.GroupScopeID), "group_concurrency": "3", "group_interval_ms": "100", "group_started": "1", "group_pending": "0", "group_active_started": "0", "group_open_jobs": "1", "audit_group_counts": "1"} {
		runLuaCollection(r.sharedLuaRedis, f.runKey+":"+name, "hash", map[string]string{"default": value})
	}
	for name, fields := range map[string][]string{"retry_reason_counts": runLuaRetryNames, "recovery_outcome_counts": runLuaRecoveryNames, "disposition_reason_counts": runLuaDispositionNames} {
		m := map[string]string{}
		for _, k := range fields {
			m[k] = "0"
		}
		runLuaCollection(r.sharedLuaRedis, f.runKey+":"+name, "hash", m)
	}
	r.setZSet(RunsKey, map[string]float64{string(lease.RunID): float64(r.now - 10000)})
	r.setSet(ActiveRunsKey, []string{string(lease.RunID)})
	r.setSet(UnarchivedRunsKey, []string{string(lease.RunID)})
	r.setSet(f.runKey+":jobs", []string{string(lease.JobID)})
	r.setZSet(f.runKey+":job_order", map[string]float64{string(lease.JobID): 0})
	r.setZSet(f.runKey+":leased", map[string]float64{string(lease.JobID): float64(r.now + 60000)})
	r.setZSet(f.runKey+":leased_at", map[string]float64{string(lease.JobID): float64(r.now - 2000)})
	r.setZSet(ActiveLeasesKey, map[string]float64{string(lease.RunID) + ":" + string(lease.JobID): float64(r.now + 60000)})
	origin, _ := DeriveCanonicalOrigin(source.CanonicalURL)
	rateInventory := map[string]float64{}
	for _, item := range []struct {
		id      Digest
		kind    string
		witness string
	}{{source.Decision.GlobalScopeID, "global", "global"}, {source.Decision.GroupScopeID, "group", string(source.RateScopeID)}, {source.Decision.OriginScopeID, "origin", string(origin)}} {
		rate := recordAuthorityRateScopeRecord(t)
		for index, value := range map[int]string{rateScopeIDIndex: string(item.id), rateScopeKindIndex: item.kind, rateScopeWitnessIndex: item.witness,
			rateScopeLastStartedAtMSIndex: canonicalDecimal(r.now - 1000), rateScopeUpdatedAtMSIndex: canonicalDecimal(r.now - 1000), rateScopeNextAllowedMSIndex: canonicalDecimal(r.now - 900)} {
			rate[index].Value = []byte(value)
		}
		if item.kind == "global" {
			rate[rateScopeEffectiveConcurrencyIndex].Value, rate[rateScopeEffectiveIntervalMSIndex].Value, rate[rateScopeNextAllowedMSIndex].Value = []byte("2"), []byte("0"), []byte("0")
		}
		if err := ValidateRecord(SchemaRateScope, rate); err != nil {
			t.Fatal("rate fixture", err)
		}
		key, _ := RateScopeKey(item.id)
		r.setHash(key, rate)
		rateInventory[string(item.id)] = float64(r.now - 1000)
	}
	r.setZSet(RateScopesKey, rateInventory)
	return f
}

func (f *stageOpsFixture) rebuildOutput(t *testing.T) {
	t.Helper()
	var err error
	f.digest, err = DeriveOutputDigest(f.context, f.output)
	if err != nil {
		t.Fatal(err)
	}
	f.publication, _ = DerivePublicationID(PublicationIdentity{RunID: f.lease.RunID, JobID: f.lease.JobID, Fence: f.lease.Fence, OutputDigest: f.digest})
	f.identity = outputCommitIdentity(f.context, f.publication)
	f.commit, _ = DeriveCommitID(f.identity)
	f.prefix = "mifolyo:crawl:v2:stage:" + string(f.commit) + ":"
	f.chunks, f.digests = buildFixtureStageChunks(t, f.commit, f.publication, f.context, f.output)
}

func (f *stageOpsFixture) wire(t *testing.T, op OperationName, chunk *StageChunk) ([]string, []string) {
	t.Helper()
	gate := runLuaGate(t, f.a, op, false)
	var request OperationWireRequest
	var err error
	switch op {
	case OperationBeginStage:
		request, err = NewBeginStageWireRequest(gate, BeginStageWireInput{Context: f.context, Output: f.output, Lease: f.lease})
	case OperationAbortStage:
		request, err = NewAbortStageWireRequest(gate, AbortStageTransitionInput{Lease: f.lease, CommitID: f.commit})
	case OperationSealStage:
		request, err = NewSealStageWireRequest(gate, SealStageWireInput{Context: f.context, Lease: f.lease, CommitID: f.commit, VerifiedOutputDigest: f.digest, VerifiedManifestChunkDigest: Digest(f.digests[string(ChunkImageManifest)][0])})
	case OperationCommit:
		request, err = NewCommitWireRequest(gate, f.identity)
	default:
		if chunk == nil {
			t.Fatal("chunk required")
		}
		constructors := map[OperationName]func(TransportGate, LeaseIdentity, StageChunk) (OperationWireRequest, error){
			OperationStagePageFields: NewStagePageFieldsWireRequest, OperationStagePageBlob: NewStagePageBlobWireRequest,
			OperationStageOutlinksBatch: NewStageOutlinksBatchWireRequest, OperationStageDiscoveriesBatch: NewStageDiscoveriesBatchWireRequest,
			OperationStageAliasesBatch: NewStageAliasesBatchWireRequest, OperationStageImagesBatch: NewStageImagesBatchWireRequest, OperationStageImageManifest: NewStageImageManifestWireRequest,
		}
		request, err = constructors[op](gate, f.lease, *chunk)
	}
	keys, args := runLuaParts(t, request, err)
	want := 117
	if op == OperationCommit {
		want = 118
	}
	if len(keys) != want {
		t.Fatalf("%s wire keys = %d, want %d", op, len(keys), want)
	}
	return keys, args
}

func (f *stageOpsFixture) call(t *testing.T, op OperationName, chunk *StageChunk) bootLuaResult {
	t.Helper()
	keys, args := f.wire(t, op, chunk)
	return stageOpsRun(t, f.r, stageOpsSource(t, op), keys, args)
}

func stageOpsAssertTrace(t *testing.T, r *stageOpsRedis) {
	t.Helper()
	if len(r.trace) == 0 || r.trace[0].name != "TIME" {
		t.Fatal("TIME not first")
	}
	times, infos, lastACL, firstWrite, writes := 0, 0, -1, -1, 0
	for i, c := range r.trace {
		if c.name == "TIME" {
			times++
		}
		if c.name == "INFO" && c.args[0] == "MEMORY" {
			infos++
		}
		if c.acl {
			lastACL = i
		} else if stageOpsWrite(c.name) {
			if firstWrite < 0 {
				firstWrite = i
			}
			writes++
		} else if firstWrite >= 0 {
			t.Fatal("read after mutation", c.name)
		}
	}
	if times != 1 || infos > 1 || (writes > 0 && (lastACL >= firstWrite || r.aclCount != writes || !r.returnedPrebuilt)) {
		t.Fatalf("unsealed execution: TIME=%d INFO=%d ACL=%d writes=%d prebuilt=%v", times, infos, r.aclCount, writes, r.returnedPrebuilt)
	}
}

func (f *stageOpsFixture) expect(t *testing.T, op OperationName, chunk *StageChunk, status Status) []any {
	t.Helper()
	got := sharedLuaNoError(t, f.call(t, op, chunk))
	if err := ValidateOperationResponse(op, got); err != nil {
		t.Fatalf("Go response oracle for %s: %v", op, err)
	}
	values := got.([]any)
	if values[0] != string(status) {
		t.Fatalf("%s: got %v want %s", op, values, status)
	}
	stageOpsAssertTrace(t, f.r)
	return values
}

func (f *stageOpsFixture) reject(t *testing.T, op OperationName, chunk *StageChunk, mutate func([]string, []string), code ErrorCode) {
	t.Helper()
	keys, args := f.wire(t, op, chunk)
	if mutate != nil {
		mutate(keys, args)
	}
	before := f.r.snapshot()
	got := stageOpsRun(t, f.r, stageOpsSource(t, op), keys, args)
	if got.runtimeErr != nil {
		t.Fatal(got.runtimeErr)
	}
	if got.raw != bootLuaErrorReply("ERR CRAWL_V2_"+string(code)) {
		t.Fatalf("%s: got %#v, want %s", op, got.raw, code)
	}
	if f.r.attempts != 0 || !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("rejection changed data/canaries")
	}
	stageOpsAssertTrace(t, f.r)
}

func stageOpsChunkOperation(kind ChunkKind) OperationName {
	return map[ChunkKind]OperationName{ChunkPageFields: OperationStagePageFields, ChunkHTML: OperationStagePageBlob, ChunkOriginalHTML: OperationStagePageBlob, ChunkOutlinks: OperationStageOutlinksBatch,
		ChunkDiscoveries: OperationStageDiscoveriesBatch, ChunkAliases: OperationStageAliasesBatch, ChunkImages: OperationStageImagesBatch, ChunkImageManifest: OperationStageImageManifest}[kind]
}

func (f *stageOpsFixture) stageAll(t *testing.T, replays bool) {
	t.Helper()
	before := f.r.snapshot()
	f.expect(t, OperationBeginStage, nil, StatusStageBegun)
	growth := stageOpsGrowth(t, before, f.r.trace)
	if f.remaining(t) != StageMemoryReservationBytes-growth {
		t.Fatal("BEGIN slot did not charge its exact self-inclusive G")
	}
	if replays {
		before := f.r.snapshot()
		f.r.now++
		f.expect(t, OperationBeginStage, nil, StatusExistsIdentical)
		if f.r.attempts != 0 || !reflect.DeepEqual(before, f.r.snapshot()) {
			t.Fatal("BEGIN replay mutated/extended TTL")
		}
	}
	for _, kind := range []ChunkKind{ChunkPageFields, ChunkHTML, ChunkOriginalHTML, ChunkOutlinks, ChunkDiscoveries, ChunkAliases, ChunkImages, ChunkImageManifest} {
		for _, chunk := range f.chunks[string(kind)] {
			before, remaining := f.r.snapshot(), f.remaining(t)
			f.expect(t, stageOpsChunkOperation(kind), &chunk, StatusStaged)
			growth := stageOpsGrowth(t, before, f.r.trace)
			if f.remaining(t) != remaining-growth {
				t.Fatal("chunk slot did not charge exact self-inclusive G", kind)
			}
			stageOpsValidateHash(t, f.r, f.prefix+"meta", SchemaStageMeta)
			if replays {
				before := f.r.snapshot()
				f.r.now++
				f.expect(t, stageOpsChunkOperation(kind), &chunk, StatusExistsIdentical)
				if f.r.attempts != 0 || !reflect.DeepEqual(before, f.r.snapshot()) {
					t.Fatal("data replay mutated", kind)
				}
			}
		}
	}
	stageOpsValidateHash(t, f.r, f.prefix+"page", SchemaFinalPage)
	stageOpsValidateHash(t, f.r, f.prefix+"image_manifest", SchemaImageManifest)
	for i := range f.output.Images {
		stageOpsValidateHash(t, f.r, f.prefix+fmt.Sprintf("image:%d", i), SchemaFinalImage)
	}
	f.verifyReadback(t)
	before, remaining := f.r.snapshot(), f.remaining(t)
	f.expect(t, OperationSealStage, nil, StatusSealed)
	if f.remaining(t) != remaining-stageOpsGrowth(t, before, f.r.trace) {
		t.Fatal("SEAL G not exact")
	}
	if replays {
		before := f.r.snapshot()
		f.r.now++
		f.expect(t, OperationSealStage, nil, StatusExistsIdentical)
		if f.r.attempts != 0 || !reflect.DeepEqual(before, f.r.snapshot()) {
			t.Fatal("SEAL replay mutated")
		}
	}
}

func (f *stageOpsFixture) remaining(t *testing.T) uint64 {
	t.Helper()
	s := strings.Split(f.r.data[StageSlotsKey].hash[string(f.commit)], ":")
	if len(s) != 5 {
		t.Fatal("missing slot")
	}
	n, err := strconv.ParseUint(s[0], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func stageOpsValidateHash(t *testing.T, r *stageOpsRedis, key string, schema RecordSchema) Record {
	t.Helper()
	entry, ok := r.data[key]
	names, err := RecordSchemaFields(schema)
	if err != nil || !ok || entry.kind != "hash" || len(entry.hash) != len(names) {
		t.Fatalf("wrong %s shape at %s", schema, key)
	}
	var record Record
	for _, name := range names {
		v, ok := entry.hash[name]
		if !ok {
			t.Fatal("missing field", name)
		}
		record = append(record, textField(name, v))
	}
	if err := ValidateRecord(schema, record); err != nil {
		t.Fatalf("Go %s validator: %v", schema, err)
	}
	return record
}

func stageOpsSameRecord(a, b Record) bool {
	left, le := EncodeRecord(a)
	right, re := EncodeRecord(b)
	return le == nil && re == nil && bytes.Equal(left, right)
}

// Independently serialize the ACTUAL stored sections with Go codecs, including
// stored aliases (never substitute context.aliases), before issuing SEAL. HTML
// hashing here is Go/client-side only, never a Lua implementation shortcut.
func (f *stageOpsFixture) verifyReadback(t *testing.T) {
	t.Helper()
	meta := stageOpsValidateHash(t, f.r, f.prefix+"meta", SchemaStageMeta)
	job := stageOpsValidateHash(t, f.r, f.jobKey, SchemaJob)
	if err := ValidateStageTranscript(OperationSealStage, job, meta, f.context, f.identity); err != nil {
		t.Fatal("Go transcript readback", err)
	}
	page := stageOpsValidateHash(t, f.r, f.prefix+"page", SchemaFinalPage)
	finalPage, err := newFinalPageRecord(page)
	if err != nil || finalPage.ValidateAgainstContext(f.context) != nil {
		t.Fatal("Go page/context readback", err)
	}
	value := func(key, name string) string {
		v, found := f.r.data[key].hash[name]
		if !found {
			t.Fatal("missing stored readback field", key, name)
		}
		return v
	}
	actual := map[ChunkKind][]Record{}
	actual[ChunkPageFields] = []Record{{page[0], page[3], page[4], page[5], page[6], page[7], page[8], page[9]}}
	actual[ChunkHTML] = []Record{{textField("field_name", "html"), page[1]}}
	actual[ChunkHTML][0][1].Name = "field_bytes"
	actual[ChunkOriginalHTML] = []Record{{textField("field_name", "original_html"), page[2]}}
	actual[ChunkOriginalHTML][0][1].Name = "field_bytes"
	var outlinks, ids []string
	for url := range f.r.sets[f.prefix+"outlinks"] {
		outlinks = append(outlinks, url)
	}
	sort.Strings(outlinks)
	for _, url := range outlinks {
		actual[ChunkOutlinks] = append(actual[ChunkOutlinks], Record{textField("target_url", url)})
	}
	for id := range f.r.zsets[f.prefix+"discoveries"] {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var semanticDiscoveries []Record
	for _, id := range ids {
		r := Record{textField("job_id", id), textField("canonical_url", value(f.prefix+"discovery_records", id+":canonical_url")),
			textField("depth", value(f.prefix+"discovery_depths", id)), textField("score_text", value(f.prefix+"discovery_records", id+":score_text"))}
		for _, name := range []string{"group_id", "rate_scope_id", "group_scope_id", "initial_origin_scope_id", "policy_decision_sha256"} {
			r = append(r, textField(name, value(f.prefix+"discovery_records", id+":"+name)))
		}
		actual[ChunkDiscoveries] = append(actual[ChunkDiscoveries], r)
		semanticDiscoveries = append(semanticDiscoveries, Record{r[0], r[1], r[2], r[3], r[4], r[5], r[8]})
	}
	ids = nil
	for name := range f.r.data[f.prefix+"aliases"].hash {
		if strings.HasSuffix(name, ":canonical_url") {
			ids = append(ids, strings.TrimSuffix(name, ":canonical_url"))
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		actual[ChunkAliases] = append(actual[ChunkAliases], Record{textField("url_id", id), textField("canonical_url", value(f.prefix+"aliases", id+":canonical_url")), textField("depth", value(f.prefix+"aliases", id+":depth"))})
	}
	n, _ := strconv.Atoi(value(f.prefix+"meta", "images_written"))
	for i := 0; i < n; i++ {
		key := f.prefix + fmt.Sprintf("image:%d", i)
		actual[ChunkImages] = append(actual[ChunkImages], Record{textField("normalized_source_url", value(key, "normalized_source_url")), textField("alt", value(key, "alt"))})
	}
	actual[ChunkImageManifest] = []Record{stageOpsValidateHash(t, f.r, f.prefix+"image_manifest", SchemaImageManifest)}
	for kind, records := range actual {
		for first, ordinal := 0, uint64(0); first < len(records); first, ordinal = first+64, ordinal+1 {
			last := min(first+64, len(records))
			chunk, err := newValidatedStageChunk(f.commit, kind, ordinal, records[first:last])
			if err != nil {
				t.Fatal("Go chunk readback", kind, err)
			}
			digest, err := DeriveChunkDigest(chunk)
			field := string(kind) + "_chunk_digest"
			if kind == ChunkOutlinks || kind == ChunkDiscoveries || kind == ChunkAliases || kind == ChunkImages {
				field = fmt.Sprintf("%s_chunk_%d_digest", kind, ordinal)
			} else if kind == ChunkImageManifest {
				field = "manifest_chunk_digest"
			}
			if err != nil || string(digest) != value(f.prefix+"meta", field) {
				t.Fatal("stored chunk differs from independently recomputed Go digest", kind, ordinal, err)
			}
		}
	}
	var sections [][]byte
	for _, section := range []struct {
		label string
		r     []Record
	}{{"page", []Record{page[:9]}}, {"outlinks", actual[ChunkOutlinks]}, {"images", actual[ChunkImages]}, {"discoveries", semanticDiscoveries}, {"aliases", actual[ChunkAliases]}} {
		encoded, err := EncodeSection(section.label, section.r)
		if err != nil {
			t.Fatal(err)
		}
		sections = append(sections, encoded)
	}
	digest := digestEncoded("mifolyo:crawl-output:v2", sections...)
	if digest != f.digest || string(digest) != value(f.prefix+"meta", "output_digest") {
		t.Fatal("actual stored output (including aliases) differs from Go output digest")
	}
}

// Generic descriptor accounting, independent of Stage transition logic. State
// evolves in CALL ORDER; no deletion credit, no old-width slot overcharge.
func stageOpsGrowth(t *testing.T, before sharedLuaSnapshot, trace []bootLuaCommand) uint64 {
	t.Helper()
	logical, keys, elements := uint64(0), uint64(0), uint64(0)
	r := &sharedLuaRedis{data: before.data, sets: before.sets, zsets: before.zsets, lists: before.lists}
	for _, c := range trace {
		if c.acl || !stageOpsWrite(c.name) {
			continue
		}
		key := c.args[0]
		e, exists := r.data[key]
		creates := c.name == "HSET" || c.name == "SADD" || c.name == "ZADD" || c.name == "LPUSH" || c.name == "RPUSH" || c.name == "SET"
		if creates && !exists {
			logical += uint64(len(key))
			keys++
			switch c.name {
			case "HSET":
				e = bootLuaEntry{kind: "hash", hash: map[string]string{}}
			case "SADD":
				e.kind, r.sets[key] = "set", map[string]bool{}
			case "ZADD":
				e.kind, r.zsets[key] = "zset", map[string]float64{}
			case "LPUSH", "RPUSH":
				e.kind = "list"
			case "SET":
				e.kind = "string"
			}
			r.data[key] = e
		}
		switch c.name {
		case "HSET":
			for i := 1; i < len(c.args); i += 2 {
				field, value := c.args[i], c.args[i+1]
				old, found := e.hash[field]
				if !found {
					logical += uint64(len(field))
					elements++
				}
				if !found || old != value {
					logical += uint64(len(value))
				}
				e.hash[field] = value
			}
		case "HDEL":
			for _, field := range c.args[1:] {
				delete(e.hash, field)
			}
			if len(e.hash) == 0 {
				r.removeKey(key)
			}
		case "SADD":
			for _, member := range c.args[1:] {
				if !r.sets[key][member] {
					logical += uint64(len(member))
					elements++
				}
				r.sets[key][member] = true
			}
		case "ZADD":
			for i := 1; i < len(c.args); i += 2 {
				score, _ := strconv.ParseFloat(c.args[i], 64)
				member := c.args[i+1]
				old, found := r.zsets[key][member]
				if !found {
					logical += uint64(len(member))
					elements++
				}
				if !found || old != score {
					logical += uint64(len(c.args[i]))
				}
				r.zsets[key][member] = score
			}
		case "ZREM", "SREM":
			for _, member := range c.args[1:] {
				delete(r.zsets[key], member)
				delete(r.sets[key], member)
			}
			if len(r.zsets[key])+len(r.sets[key]) == 0 {
				r.removeKey(key)
			}
		case "LPUSH", "RPUSH":
			for _, value := range c.args[1:] {
				logical += uint64(len(value))
				elements++
				r.lists[key] = append(r.lists[key], value)
			}
		case "RENAME":
			dest := c.args[1]
			if !exists || r.data[dest].kind != "" {
				t.Fatal("unvalidated rename in trace")
			}
			logical += uint64(len(key) + len(dest))
			keys++
			r.data[dest], r.sets[dest], r.zsets[dest], r.lists[dest] = e, r.sets[key], r.zsets[key], r.lists[key]
			r.removeKey(key)
		case "UNLINK":
			r.removeKey(key)
		case "PERSIST", "PEXPIREAT":
		case "SET":
			if !exists || e.value != c.args[1] {
				logical += uint64(len(c.args[1]))
			}
			e.value = c.args[1]
			r.data[key] = e
		default:
			t.Fatal("unknown command in growth oracle", c.name)
		}
	}
	return 3*logical + 1024*keys + 256*elements
}

func TestStageOperationsWireAndPrebuiltTails(t *testing.T) {
	t.Parallel()
	f := stageOpsFixtureNew(t, 1, 1, 1)
	for _, op := range stageOpsInventory {
		var chunk *StageChunk
		for kind, chunks := range f.chunks {
			if stageOpsChunkOperation(ChunkKind(kind)) == op && len(chunks) > 0 {
				v := chunks[0]
				chunk = &v
				break
			}
		}
		keys, args := f.wire(t, op, chunk)
		prefix := stageOpsCore(t)
		// Actual rev4 clocked decoder, not a Go mirror of its key plan.
		probe := prefix + `local spec,code=CJ.Wire.stage_spec(ARGV[1])
if not spec then return CJ.Context.reject(code) end
local args={}; for i=2,#ARGV do args[i-1]=ARGV[i] end
local ctx,err=CJ.Context.open(spec,KEYS,args)
if not ctx then return CJ.Context.reject(err) end
return {ctx.operation,P.format_decimal(#KEYS),ctx.request.v.commit_id}`
		got := sharedLuaNoError(t, stageOpsRun(t, f.r, probe, keys, append([]string{string(op)}, args...)))
		if !reflect.DeepEqual(got, []any{string(op), strconv.Itoa(len(keys)), string(f.commit)}) {
			t.Fatal("wire decoder mismatch", op, got)
		}
		fragment := string(primitiveLuaRead(t, "lua_src/ops/"+strings.ToLower(string(op))+".lua"))
		tail := "for i=1,execution.count do\n    redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))\nend\nreturn execution.reply\n"
		if !strings.HasSuffix(fragment, tail) || strings.Count(fragment, "redis.call(") != 1 {
			t.Fatal("mutation tail drift", op)
		}
	}
}

func TestStageOperationsPublicationAndReplays(t *testing.T) {
	t.Parallel()
	for _, images := range []int{0, 2} {
		t.Run(fmt.Sprint(images), func(t *testing.T) {
			f := stageOpsFixtureNew(t, 2, 2, images)
			f.stageAll(t, true)
			f.expect(t, OperationCommit, nil, StatusCommitted)
			stageOpsPublicationOrder(t, f)
			pageKey, _ := PageDataKey(f.publication, f.output.Page.NormalizedURL)
			manifestKey, _ := PageImagesKey(f.publication, f.output.Page.NormalizedURL)
			outlinksKey, _ := OutlinksKey(f.publication, f.output.Page.NormalizedURL)
			if !reflect.DeepEqual(f.r.lists[PagesQueueKey], []string{pageKey}) || len(f.r.lists[ImageIndexerQueueKey]) != 0 {
				t.Fatal("wrong notification or direct image enqueue")
			}
			wantPage, err := NewFinalPageRecord(f.context, f.output.Page, f.publication)
			if err != nil {
				t.Fatal(err)
			}
			want, _ := wantPage.Record()
			if got := stageOpsValidateHash(t, f.r, pageKey, SchemaFinalPage); !stageOpsSameRecord(got, want) {
				t.Fatal("final page differs from Go output")
			}
			manifest, _ := NewImageManifestRecord(f.publication, f.output.Page.NormalizedURL, f.output.Images)
			want, _ = manifest.Record()
			if got := stageOpsValidateHash(t, f.r, manifestKey, SchemaImageManifest); !stageOpsSameRecord(got, want) {
				t.Fatal("manifest differs from Go")
			}
			for _, key := range append([]string{pageKey, manifestKey, outlinksKey}, manifest.ImageKeys()...) {
				if f.r.data[key].expireAt > 0 {
					t.Fatal("final output retained stage TTL", key)
				}
			}
			for _, target := range f.output.Outlinks {
				key, _ := BacklinksKey(target)
				if !f.r.sets[key][f.output.Page.NormalizedURL] {
					t.Fatal("missing backlink")
				}
			}
			for _, discovery := range f.output.Discoveries {
				key, _ := RunJobKey(f.lease.RunID, discovery.JobID)
				job := stageOpsValidateHash(t, f.r, key, SchemaJob)
				if string(job[jobLeaseRequestStartsBaselineIndex].Value) != "0" || string(job[jobRequestStartsIndex].Value) != "0" || string(job[jobStateIndex].Value) != "ready" {
					t.Fatal("discovery initialized with execution history")
				}
			}
			stageOpsValidateHash(t, f.r, f.jobKey, SchemaJob)
			stageOpsValidateHash(t, f.r, f.runKey, SchemaRun)
			if _, ok := f.r.data[StageSlotsKey].hash[string(f.commit)]; ok {
				t.Fatal("committed slot retained")
			}
			for _, suffix := range []string{"meta", "keys", "aliases", "discoveries", "discovery_records", "discovery_depths"} {
				if f.r.data[f.prefix+suffix].expireAt != int64(f.r.now+60000) {
					t.Fatal("residual expiry", suffix)
				}
			}
			before := f.r.snapshot()
			f.r.now++
			f.expect(t, OperationCommit, nil, StatusAlreadyCommitted)
			if f.r.attempts != 0 || !reflect.DeepEqual(before, f.r.snapshot()) {
				t.Fatal("COMMIT replay changed notification/publication/ledger")
			}
			// Delete consumer output and corrupt unrelated admission state: a
			// retained receipt must still precede Run.load, queue and stage reads.
			for _, key := range append([]string{pageKey, outlinksKey, manifestKey}, manifest.ImageKeys()...) {
				f.r.removeKey(key)
			}
			f.r.data[f.runKey+":group_limits"] = bootLuaEntry{kind: "string", value: "not-a-map", expireAt: -1}
			f.r.data[PagesQueueKey] = bootLuaEntry{kind: "string", value: "not-a-queue", expireAt: -1}
			before = f.r.snapshot()
			f.expect(t, OperationCommit, nil, StatusAlreadyCommitted)
			if !reflect.DeepEqual(before, f.r.snapshot()) {
				t.Fatal("retained receipt recreated output")
			}
			for _, c := range f.r.trace {
				if len(c.args) > 0 && (strings.HasPrefix(c.args[0], f.prefix) || c.args[0] == PagesQueueKey || c.args[0] == f.runKey+":group_limits") {
					t.Fatal("replay crossed admission read boundary", c)
				}
			}
		})
	}
}

func stageOpsPublicationOrder(t *testing.T, f *stageOpsFixture) {
	t.Helper()
	var calls []bootLuaCommand
	for _, c := range f.r.trace {
		if !c.acl && stageOpsWrite(c.name) {
			calls = append(calls, c)
		}
	}
	page, _ := PageDataKey(f.publication, f.output.Page.NormalizedURL)
	outlinks, _ := OutlinksKey(f.publication, f.output.Page.NormalizedURL)
	manifest, _ := PageImagesKey(f.publication, f.output.Page.NormalizedURL)
	moves := [][2]string{{f.prefix + "page", page}}
	if len(f.output.Outlinks) > 0 {
		moves = append(moves, [2]string{f.prefix + "outlinks", outlinks})
	}
	for i, image := range f.output.Images {
		key, _ := ImageDataKey(f.publication, f.output.Page.NormalizedURL, image.NormalizedSourceURL)
		moves = append(moves, [2]string{f.prefix + fmt.Sprintf("image:%d", i), key})
	}
	moves = append(moves, [2]string{f.prefix + "image_manifest", manifest})
	for i, pair := range moves {
		if calls[2*i].name != "RENAME" || !reflect.DeepEqual(calls[2*i].args, []string{pair[0], pair[1]}) || calls[2*i+1].name != "PERSIST" ||
			!reflect.DeepEqual(calls[2*i+1].args, []string{pair[1]}) {
			t.Fatal("rename/persist order", i)
		}
	}
	notifications, notifyIndex, completedIndex := 0, -1, -1
	for i, c := range calls {
		if c.name == "LPUSH" && c.args[0] == PagesQueueKey {
			notifications++
			notifyIndex = i
			if !reflect.DeepEqual(c.args, []string{PagesQueueKey, page}) {
				t.Fatal("noncanonical notification")
			}
		}
		if c.name == "HSET" && c.args[0] == f.jobKey {
			for n := 1; n < len(c.args); n += 2 {
				if c.args[n] == "state" && c.args[n+1] == "completed" {
					completedIndex = i
				}
			}
		}
		if c.name == "PEXPIREAT" {
			if !strings.HasPrefix(c.args[0], f.prefix) || strings.HasPrefix(c.args[0], f.prefix+"image:") || c.args[0] == f.prefix+"page" || c.args[0] == f.prefix+"outlinks" || c.args[0] == f.prefix+"image_manifest" {
				t.Fatal("commit TTL touched renamed/final/unrelated output")
			}
		}
	}
	last := calls[len(calls)-1]
	if notifications != 1 || completedIndex <= notifyIndex || last.name != "HDEL" || !reflect.DeepEqual(last.args, []string{StageSlotsKey, string(f.commit)}) {
		t.Fatal("notification/completion/core slot-release order")
	}
}

func (f *stageOpsFixture) clone() *stageOpsFixture {
	s := f.r.snapshot()
	c := *f
	c.r = &stageOpsRedis{sharedLuaRedis: &sharedLuaRedis{data: s.data, sets: s.sets, zsets: s.zsets, lists: s.lists,
		now: f.r.now, runID: f.r.runID, used: f.r.used, maximum: f.r.maximum, lazyfree: f.r.lazyfree, writes: s.writes}}
	return &c
}

func TestStageOperationsBeginFreezeAndCapacity(t *testing.T) {
	t.Parallel()
	f := stageOpsFixtureNew(t, 0, 0, 0)
	f.reject(t, OperationBeginStage, nil, func(_, args []string) { args[16] = "2" }, ErrorCode("STAGE_INVALID"))
	f.reject(t, OperationBeginStage, nil, func(_, args []string) { args[15] = "00" }, ErrorCode("INVALID_NUMBER"))
	f.reject(t, OperationBeginStage, nil, func(_, args []string) { args[15], args[16] = "1", "1" }, ErrorCode("INVALID_ARGUMENT"))
	f.expect(t, OperationBeginStage, nil, StatusStageBegun)
	f.reject(t, OperationBeginStage, nil, func(_, args []string) { args[16] = "2" }, ErrorCode("IMMUTABLE_MISMATCH"))
	f.reject(t, OperationBeginStage, nil, func(_, args []string) { args[14] = strings.Repeat("f", 64) }, ErrorCode("IMMUTABLE_MISMATCH"))
	for _, memoryDelta := range []uint64{0, 1} {
		f := stageOpsFixtureNew(t, 0, 0, 0)
		f.r.maximum = f.r.used + StageMemoryReservationBytes + CommitMemoryReservationBytes + LeaseSafetyReservationBytes - memoryDelta
		before := f.r.snapshot()
		if memoryDelta == 0 {
			f.expect(t, OperationBeginStage, nil, StatusStageBegun)
		} else {
			got := f.expect(t, OperationBeginStage, nil, StatusStageCapacityBlocked)
			if got[2] != "memory_headroom_low" || !reflect.DeepEqual(before, f.r.snapshot()) {
				t.Fatal("BEGIN headroom block mutated/froze")
			}
		}
	}
	f = stageOpsFixtureNew(t, 0, 0, 0)
	slots := Record{}
	for i := 1; i <= 4; i++ {
		slots = append(slots, textField(fmt.Sprintf("%064x", i), "50331648:"+runLuaID+":"+fmt.Sprintf("%064x", i)+":1:0"))
	}
	f.r.setHash(StageSlotsKey, slots)
	before := f.r.snapshot()
	got := f.expect(t, OperationBeginStage, nil, StatusStageCapacityBlocked)
	if got[2] != "stage_slots_full" || got[3] != "4" || !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("full slots result or freeze")
	}
}

func TestStageOperationsAbortTerminalReceipt(t *testing.T) {
	t.Parallel()
	for _, sealed := range []bool{false, true} {
		t.Run(fmt.Sprint(sealed), func(t *testing.T) {
			f := stageOpsFixtureNew(t, 1, 1, 1)
			if sealed {
				f.stageAll(t, false)
			} else {
				f.expect(t, OperationBeginStage, nil, StatusStageBegun)
			}
			originalCount := f.r.data[f.prefix+"meta"].hash["key_count"]
			before, remaining := f.r.snapshot(), f.remaining(t)
			f.expect(t, OperationAbortStage, nil, StatusStageAborted)
			if f.remaining(t) != remaining-stageOpsGrowth(t, before, f.r.trace) {
				t.Fatal("abort reserve is not exact")
			}
			if !strings.HasSuffix(f.r.data[StageSlotsKey].hash[string(f.commit)], ":"+originalCount) {
				t.Fatal("abort lost original count")
			}
			for _, key := range wireOracleStageKeys(f.commit) {
				if _, exists := f.r.data[key]; exists {
					t.Fatal("abort left an inventoried key")
				}
			}
			if _, exists := f.r.zsets[StageExpiryKey][string(f.commit)]; exists {
				t.Fatal("abort retained expiry member")
			}
			job := stageOpsValidateHash(t, f.r, f.jobKey, SchemaJob)
			if string(job[jobActiveStageCommitIDIndex].Value) != "" || string(job[jobLastStageCommitIDIndex].Value) != string(f.commit) || string(job[jobLastStageFenceIndex].Value) != "1" ||
				string(job[jobLeaseRequestStartsBaselineIndex].Value) != "0" || string(job[jobRequestStartsIndex].Value) != "1" {
				t.Fatal("abort reopened execution/freeze")
			}
			before = f.r.snapshot()
			f.r.now++
			got := f.expect(t, OperationAbortStage, nil, StatusExistsIdentical)
			if got[3] != originalCount || f.r.attempts != 0 || !reflect.DeepEqual(before, f.r.snapshot()) {
				t.Fatal("abort replay allocation or wrong count")
			}
			f.reject(t, OperationBeginStage, nil, nil, ErrorCode("INVALID_STATE"))
			blob := f.chunks[string(ChunkHTML)][0]
			f.reject(t, OperationStagePageBlob, &blob, nil, ErrorCode("STAGE_INVALID"))
		})
	}
}

func TestStageOperationsInputAndAuthorizationRejections(t *testing.T) {
	t.Parallel()
	f := stageOpsFixtureNew(t, 2, 2, 2)
	f.expect(t, OperationBeginStage, nil, StatusStageBegun)
	for _, kind := range []ChunkKind{ChunkOutlinks, ChunkDiscoveries, ChunkImages} {
		chunk := f.chunks[string(kind)][0]
		f.reject(t, stageOpsChunkOperation(kind), &chunk, func(_, args []string) {
			args[len(args)-1] = args[len(args)-2] // same count/digest, duplicate record
		}, ErrorCode("INVALID_ARGUMENT"))
	}
	page := f.chunks[string(ChunkPageFields)][0]
	f.expect(t, OperationStagePageFields, &page, StatusStaged)
	f.reject(t, OperationStagePageFields, &page, func(_, args []string) {
		r := page.Records()[0]
		r[2].Value = []byte("201")
		encoded, _ := EncodeRecord(r)
		args[len(args)-1] = string(encoded) // deliberately retain original digest
	}, ErrorCode("IMMUTABLE_MISMATCH"))
	f.reject(t, OperationSealStage, nil, func(_, args []string) { args[len(args)-1] = strings.Repeat("f", 64) }, ErrorCode("STAGE_INVALID"))
	for _, mode := range []string{"cancelled", "expired"} {
		c := stageOpsFixtureNew(t, 0, 0, 0)
		c.stageAll(t, false)
		status := StatusAuthorizationExpired
		if mode == "cancelled" {
			status = StatusRunCancelled
			c.r.data[c.runKey].hash["state"] = "cancelled"
			c.r.data[c.runKey].hash["cancelled_at_ms"] = canonicalDecimal(c.r.now)
			c.r.data[c.runKey].hash["terminal_reason"] = "operator_cancelled"
		} else {
			c.r.data[c.runKey].hash["authorization_expires_at_ms"] = canonicalDecimal(c.r.now)
		}
		before := c.r.snapshot()
		c.expect(t, OperationCommit, nil, status)
		if !reflect.DeepEqual(before, c.r.snapshot()) || c.r.attempts != 0 {
			t.Fatal("unauthorized publication changed state")
		}
		// Safety cleanup of an owned stage must still work after authorization
		// ends; it does not reopen requests or clear the retained freeze.
		c.expect(t, OperationAbortStage, nil, StatusStageAborted)
	}
	c := stageOpsFixtureNew(t, 0, 0, 0)
	c.r.data[c.jobKey].hash["active_reservation_id"] = strings.Repeat("f", 64)
	c.reject(t, OperationBeginStage, nil, nil, ErrorCode("INVALID_STATE"))
}

func TestStageOperationsFailureSnapshotsAndLabels(t *testing.T) {
	t.Parallel()
	f := stageOpsFixtureNew(t, 1, 1, 2)
	f.stageAll(t, false)
	for _, test := range []struct {
		name string
		code ErrorCode
		edit func(*stageOpsFixture)
	}{
		{"B", ErrorCode("COUNTER_CORRUPT"), func(f *stageOpsFixture) { f.r.data[f.prefix+"meta"].hash["request_starts_baseline"] = "1" }},
		{"G", ErrorCode("COUNTER_CORRUPT"), func(f *stageOpsFixture) { f.r.data[f.prefix+"meta"].hash["request_starts_generation"] = "2" }},
		{"source", ErrorCode("STAGE_INVALID"), func(f *stageOpsFixture) {
			f.r.data[f.prefix+"page"].hash["normalized_url"] = "https://example.com/other"
		}},
		{"timestamp", ErrorCode("STAGE_INVALID"), func(f *stageOpsFixture) {
			f.r.data[f.prefix+"page"].hash["last_crawled"] = "Thu, 01 Jan 1970 00:00:00 UTC"
		}},
		{"missing-meta-field", ErrorCode("STAGE_INVALID"), func(f *stageOpsFixture) { delete(f.r.data[f.prefix+"meta"].hash, "abandoned") }},
		{"missing-page-field", ErrorCode("STAGE_INVALID"), func(f *stageOpsFixture) { delete(f.r.data[f.prefix+"page"].hash, "html") }},
		{"no-ttl", ErrorCode("STAGE_INVALID"), func(f *stageOpsFixture) {
			e := f.r.data[f.prefix+"aliases"]
			e.expireAt = -1
			f.r.data[f.prefix+"aliases"] = e
		}},
		{"extended-ttl", ErrorCode("STAGE_INVALID"), func(f *stageOpsFixture) { e := f.r.data[f.prefix+"page"]; e.expireAt++; f.r.data[f.prefix+"page"] = e }},
		{"inventory-path", ErrorCode("STAGE_INVALID"), func(f *stageOpsFixture) { f.r.lists[f.prefix+"keys"][0] = f.prefix + "../outside" }},
		{"bad-counter", ErrorCode("COUNTER_CORRUPT"), func(f *stageOpsFixture) { f.r.data[f.runKey+":group_open_jobs"].hash["default"] = "0" }},
		{"late-image-destination", ErrorCode("DESTINATION_EXISTS"), func(f *stageOpsFixture) {
			key, _ := ImageDataKey(f.publication, f.output.Page.NormalizedURL, f.output.Images[1].NormalizedSourceURL)
			f.r.setHash(key, Record{textField("canary", "must-not-overwrite")})
		}},
		{"late-discovery-type", ErrorCode("WRONG_TYPE"), func(f *stageOpsFixture) {
			key, _ := RunJobKey(f.lease.RunID, f.output.Discoveries[0].JobID)
			f.r.data[key] = bootLuaEntry{kind: "string", value: "canary", expireAt: -1}
		}},
		{"published-counter-without-completion", ErrorCode("INVALID_STATE"), func(f *stageOpsFixture) {
			f.r.data[f.runKey].hash["output_commits_total"] = "1"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := f.clone()
			test.edit(c)
			c.reject(t, OperationCommit, nil, nil, test.code)
		})
	}
	for _, op := range []OperationName{OperationBeginStage, OperationSealStage, OperationAbortStage, OperationCommit} {
		c := f.clone()
		c.r.now += 60001
		before := c.r.snapshot()
		c.expect(t, op, nil, StatusLeaseLost)
		if c.r.attempts != 0 || !reflect.DeepEqual(before, c.r.snapshot()) {
			t.Fatal("expired lease mutated state", op)
		}
	}
	u := stageOpsFixtureNew(t, 0, 0, 0)
	u.expect(t, OperationBeginStage, nil, StatusStageBegun)
	u.reject(t, OperationCommit, nil, nil, ErrorCode("STAGE_UNSEALED"))
	u.reject(t, OperationSealStage, nil, nil, ErrorCode("STAGE_INVALID"))
}

func TestStageOperationsLateACLAndExecutorFailure(t *testing.T) {
	t.Parallel()
	f := stageOpsFixtureNew(t, 2, 2, 2)
	f.stageAll(t, false)
	success := f.clone()
	success.expect(t, OperationCommit, nil, StatusCommitted)
	count := success.r.attempts
	if count < 20 {
		t.Fatal("incomplete commit descriptor sequence")
	}
	for denied := 1; denied <= count; denied++ {
		c := f.clone()
		c.r.denyAt = denied
		c.reject(t, OperationCommit, nil, nil, ErrorCode("BOOT_UNAPPROVED"))
		if c.r.aclCount != denied {
			t.Fatal("did not reach exact denied descriptor")
		}
	}
	for _, after := range []bool{false, true} {
		c := f.clone()
		c.r.failAt, c.r.failAfter = 3, after
		before := c.r.snapshot()
		result := c.call(t, OperationCommit, nil)
		if result.runtimeErr == nil || !strings.Contains(result.runtimeErr.Error(), bootLuaWriteErr) || reflect.DeepEqual(before, c.r.snapshot()) {
			t.Fatal("unexpected executor error was swallowed or presented as rollback")
		}
	}
}

func (f *stageOpsFixture) fullQueue() {
	values := make([]string, 5000)
	for i := range values {
		values[i] = "page_data:" + strings.Repeat("a", 64) + ":aHR0cHM6Ly9leGFtcGxlLmNvbS8"
	}
	f.r.setList(PagesQueueKey, values)
}

func TestStageOperationsBackpressureDeadlineAndControlFloor(t *testing.T) {
	t.Parallel()
	f := stageOpsFixtureNew(t, 0, 0, 0)
	f.stageAll(t, false)
	f.fullQueue()
	preBlock := f.clone()
	before, remaining := f.r.snapshot(), f.remaining(t)
	first := f.expect(t, OperationCommit, nil, StatusDownstreamBackpressure)
	if first[2] != "pages_queue_full" || f.remaining(t) != remaining-stageOpsGrowth(t, before, f.r.trace) {
		t.Fatal("backpressure reason/G")
	}
	// Independent final-descriptor accounting at a FIVE-digit remainder. This
	// catches the old-width-overcharge bug even though legitimate histories keep
	// eight digits. No numeric G is supplied to any Lua/core API.
	trace := make([]bootLuaCommand, len(f.r.trace))
	for i, c := range f.r.trace {
		trace[i] = c
		trace[i].args = append([]string(nil), c.args...)
		if c.name == "HSET" && c.args[0] == StageSlotsKey {
			parts := strings.Split(trace[i].args[2], ":")
			parts[0] = "32768"
			trace[i].args[2] = strings.Join(parts, ":")
		}
	}
	floorCost := stageOpsGrowth(t, preBlock.r.snapshot(), trace)
	for _, delta := range []uint64{0, 1} {
		c := preBlock.clone()
		parts := strings.Split(c.r.data[StageSlotsKey].hash[string(c.commit)], ":")
		parts[0] = canonicalDecimal(TerminalStageControlFloorBytes + floorCost - delta)
		c.r.data[StageSlotsKey].hash[string(c.commit)] = strings.Join(parts, ":")
		if delta == 0 {
			c.expect(t, OperationCommit, nil, StatusDownstreamBackpressure)
			if c.remaining(t) != TerminalStageControlFloorBytes {
				t.Fatal("five-digit exact fixed point was overcharged")
			}
		} else {
			c.reject(t, OperationCommit, nil, nil, ErrorCode("MEMORY_HEADROOM_LOW"))
		}
	}
	noFixedPoint := preBlock.clone()
	fixedParts := strings.Split(noFixedPoint.r.data[StageSlotsKey].hash[string(noFixedPoint.commit)], ":")
	// G(5)=floorCost; G(6)=floorCost+3. Candidates are respectively
	// 100002 (six digits) and 99999 (five digits): neither solves its width.
	fixedParts[0] = canonicalDecimal(floorCost + 100002)
	noFixedPoint.r.data[StageSlotsKey].hash[string(noFixedPoint.commit)] = strings.Join(fixedParts, ":")
	noFixedPoint.reject(t, OperationCommit, nil, nil, ErrorCode("INVALID_STATE"))
	blocked := f.clone()
	before = f.r.snapshot()
	f.r.now++
	replayed := f.expect(t, OperationCommit, nil, StatusDownstreamBackpressure)
	if !reflect.DeepEqual(first[2:], replayed[2:]) || !reflect.DeepEqual(before, f.r.snapshot()) || f.r.attempts != 0 {
		t.Fatal("backpressure replay extended/debited")
	}
	// A normal worker renewal would retain exactly these index/record deadlines.
	deadline, _ := strconv.ParseUint(first[4].(string), 10, 64)
	f.r.data[f.jobKey].hash["lease_expires_at_ms"] = canonicalDecimal(deadline + 1)
	f.r.zsets[f.runKey+":leased"][string(f.lease.JobID)] = float64(deadline + 1)
	f.r.zsets[ActiveLeasesKey][runLuaID+":"+string(f.lease.JobID)] = float64(deadline + 1)
	f.r.now = deadline
	f.r.removeKey(PagesQueueKey)
	before = f.r.snapshot()
	replayed = f.expect(t, OperationCommit, nil, StatusDownstreamBackpressure)
	if !reflect.DeepEqual(first[2:], replayed[2:]) || !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("elapsed deadline permitted publication")
	}
	f.expect(t, OperationAbortStage, nil, StatusStageAborted)
	if f.r.data[f.jobKey].hash["commit_backpressure_deadline_ms"] != first[4] {
		t.Fatal("abort erased backpressure evidence")
	}
	// Before the deadline, restored capacity can publish and clear durable block
	// evidence in the same transaction, without changing B/G/history.
	blocked.r.removeKey(PagesQueueKey)
	blocked.r.now++
	blocked.expect(t, OperationCommit, nil, StatusCommitted)
	if blocked.r.data[blocked.jobKey].hash["commit_backpressure_reason"] != "none" || len(blocked.r.zsets[blocked.runKey+":commit_backpressure"]) != 0 {
		t.Fatal("publication retained backpressure")
	}
	low := stageOpsFixtureNew(t, 0, 0, 0)
	low.stageAll(t, false)
	low.fullQueue()
	parts := strings.Split(low.r.data[StageSlotsKey].hash[string(low.commit)], ":")
	parts[0] = "32768"
	low.r.data[StageSlotsKey].hash[string(low.commit)] = strings.Join(parts, ":")
	low.reject(t, OperationCommit, nil, nil, ErrorCode("MEMORY_HEADROOM_LOW"))
}

func TestStageOperationsMemoryBlockControlAdmissionFailClosed(t *testing.T) {
	t.Parallel()
	f := stageOpsFixtureNew(t, 0, 0, 0)
	f.stageAll(t, false)
	// Full commit would exceed headroom. The current core ALSO requires the full
	// base inequality for a first control record. That failed control admission
	// must return MEMORY_HEADROOM_LOW without partially recording/publishing.
	// This tests fail-closed behavior, NOT successful memory-triggered BP receipt.
	f.r.maximum = f.r.used + f.remaining(t) + LeaseSafetyReservationBytes
	f.reject(t, OperationCommit, nil, nil, ErrorCode("MEMORY_HEADROOM_LOW"))
	for _, c := range f.r.trace {
		if !c.acl && (c.name == "RENAME" || c.name == "LPUSH") {
			t.Fatal("blocked commit published")
		}
	}
	before := f.r.snapshot()
	f.r.now++
	f.reject(t, OperationCommit, nil, nil, ErrorCode("MEMORY_HEADROOM_LOW"))
	if !reflect.DeepEqual(before, f.r.snapshot()) || f.r.attempts != 0 {
		t.Fatal("memory block replay changed first record")
	}
}

func TestStageOperationsFirstAdmissionWins(t *testing.T) {
	t.Parallel()
	f := stageOpsFixtureNew(t, 0, 1, 0)
	d := f.output.Discoveries[0]
	old := jobLuaSourceValue(t, d.CanonicalURL)
	old.ScoreText = "-2"
	old.Depth, old.Decision.Depth = 9, 9
	job := recordsLuaInitial(t, old, f.r.now-500)
	key, _ := RunJobKey(f.lease.RunID, old.JobID)
	f.r.setHash(key, job)
	f.r.sets[f.runKey+":jobs"][string(old.JobID)] = true
	f.r.zsets[f.runKey+":job_order"][string(old.JobID)] = 0
	f.r.setZSet(f.runKey+":ready", map[string]float64{string(old.JobID): -2})
	f.r.setZSet(f.runKey+":ready_at", map[string]float64{string(old.JobID): float64(f.r.now - 500)})
	f.r.data[f.runKey].hash["job_count"], f.r.data[f.runKey].hash["open_job_count"] = "2", "2"
	f.r.data[f.runKey+":group_open_jobs"].hash["default"] = "2"
	before := f.r.snapshot().data[key]
	f.stageAll(t, false)
	f.expect(t, OperationCommit, nil, StatusCommitted)
	if !reflect.DeepEqual(before, f.r.data[key]) || f.r.zsets[f.runKey+":ready"][string(old.JobID)] != -2 || f.r.data[f.runKey].hash["job_count"] != "2" {
		t.Fatal("later discovery rewrote first score/depth/source/history")
	}
}

func stageOpsPriorAlias(t *testing.T, f *stageOpsFixture, depth string) {
	t.Helper()
	source := jobLuaSourceValue(t, "https://example.com/prior")
	prior := recordAuthorityPublishedJobRecord(t)
	sr, _ := completeSourceJobRecord(source)
	jobLuaApplySource(prior, sr)
	pub, _ := DerivePublicationID(PublicationIdentity{RunID: f.lease.RunID, JobID: source.JobID, Fence: 2, OutputDigest: Digest(prior[jobOutputDigestIndex].Value)})
	commit, _ := DeriveCommitID(CommitIdentity{RunID: f.lease.RunID, JobID: source.JobID, OwnerID: f.lease.OwnerID, Token: f.lease.Token, Fence: 2, PublicationID: pub, RequestStartsBaseline: 0, RequestStartsGeneration: 3})
	page, _ := PageDataKey(pub, string(prior[jobLastDocumentTargetURLIndex].Value))
	for index, value := range map[int]string{jobPublicationIDIndex: string(pub), jobCommitIDIndex: string(commit), jobLastStageCommitIDIndex: string(commit), jobPublishedPageKeyIndex: page,
		jobRequestStartsIndex: "3", jobNextRequestOrdinalIndex: "5", jobCreatedAtMSIndex: canonicalDecimal(f.r.now - 7000), jobUpdatedAtMSIndex: canonicalDecimal(f.r.now - 1500),
		jobCompletedAtMSIndex: canonicalDecimal(f.r.now - 1500), jobLastRequestStartedAtMSIndex: canonicalDecimal(f.r.now - 2000), jobLastDocumentRequestStartedAtMSIndex: canonicalDecimal(f.r.now - 2000)} {
		prior[index].Value = []byte(value)
	}
	if err := ValidateRecord(SchemaJob, prior); err != nil {
		t.Fatal(err)
	}
	key, _ := RunJobKey(f.lease.RunID, source.JobID)
	f.r.setHash(key, prior)
	f.r.sets[f.runKey+":jobs"][string(source.JobID)] = true
	f.r.zsets[f.runKey+":job_order"][string(source.JobID)] = 0
	f.r.setZSet(f.runKey+":completed", map[string]float64{string(source.JobID): float64(f.r.now - 1500)})
	cursor := string(source.JobID)
	if string(f.lease.JobID) > cursor {
		cursor = string(f.lease.JobID)
	}
	sourceDigest, _ := DeriveSourceDigest([]SourceJob{f.source, source})
	for name, value := range map[string]string{"job_count": "2", "expected_seed_count": "2", "completed_total": "1", "output_commits_total": "1", "request_starts": "4", "reservation_creations_total": "5", "claims_total": "3", "recovered_leases_total": "1",
		"load_revision": "2", "audit_revision": "2", "audit_count": "2", "audit_cursor": cursor, "source_sha256": string(sourceDigest), "last_terminal_transition_at_ms": canonicalDecimal(f.r.now - 1500)} {
		f.r.data[f.runKey].hash[name] = value
	}
	f.r.data[f.runKey+":group_started"].hash["default"] = "4"
	f.r.data[f.runKey+":audit_group_counts"].hash["default"] = "2"
	f.r.data[f.runKey+":disposition_reason_counts"].hash["published"] = "1"
	f.r.data[f.runKey+":recovery_outcome_counts"].hash["ready"] = "1"
	f.r.setHash(f.runKey+":visited_depth", Record{textField(string(f.lease.JobID), depth)})
	f.r.setHash(f.runKey+":visited_urls", Record{textField(string(f.lease.JobID), f.source.CanonicalURL)})
	stageOpsValidateHash(t, f.r, f.runKey, SchemaRun)
}

func TestStageOperationsAliasWitnessAndShallowestDepth(t *testing.T) {
	t.Parallel()
	for _, depth := range []string{"0", "9"} {
		f := stageOpsFixtureNew(t, 0, 0, 0)
		stageOpsPriorAlias(t, f, depth)
		f.stageAll(t, false)
		for _, mutation := range []string{"url", "depth", "missing-pair"} {
			c := f.clone()
			expected := ErrorCode("URL_ID_COLLISION")
			switch mutation {
			case "url":
				c.r.data[c.runKey+":visited_urls"].hash[string(c.lease.JobID)] = "https://example.com/different"
			case "depth":
				c.r.data[c.runKey+":visited_depth"].hash[string(c.lease.JobID)] = "00"
				expected = ErrorCode("COUNTER_CORRUPT")
			case "missing-pair":
				delete(c.r.data[c.runKey+":visited_urls"].hash, string(c.lease.JobID))
				c.r.data[c.runKey+":visited_urls"].hash[strings.Repeat("a", 64)] = "https://example.com/other"
				expected = ErrorCode("STATE_INDEX_CORRUPT")
			}
			c.reject(t, OperationCommit, nil, nil, expected)
		}
		f.expect(t, OperationCommit, nil, StatusCommitted)
		if f.r.data[f.runKey+":visited_depth"].hash[string(f.lease.JobID)] != "0" || len(f.r.data[f.runKey+":visited_depth"].hash) != 1 {
			t.Fatal("alias witness was duplicated or shallowest depth lost")
		}
	}
}

func TestStageOperationsMaximumCountsAndBlobBound(t *testing.T) {
	t.Parallel()
	t.Run("collections", func(t *testing.T) {
		f := stageOpsFixtureNew(t, 256, 128, 64)
		f.stageAll(t, false)
		if f.r.data[f.prefix+"meta"].hash["key_count"] != "73" || len(f.r.lists[f.prefix+"keys"]) != 73 {
			t.Fatal("maximum stage inventory")
		}
		f.expect(t, OperationCommit, nil, StatusCommitted)
		if f.r.data[f.runKey].hash["job_count"] != "129" || len(f.r.lists[PagesQueueKey]) != 1 {
			t.Fatal("maximum first commit truncated or duplicated")
		}
	})
	t.Run("blob", func(t *testing.T) {
		f := stageOpsFixtureNew(t, 0, 0, 0)
		f.output.Page.HTML = []byte(strings.Repeat("é", MaxPageBlobBytes/2))
		f.rebuildOutput(t)
		f.expect(t, OperationBeginStage, nil, StatusStageBegun)
		chunk := f.chunks[string(ChunkHTML)][0]
		before, remaining := f.r.snapshot(), f.remaining(t)
		f.expect(t, OperationStagePageBlob, &chunk, StatusStaged)
		if f.remaining(t) != remaining-stageOpsGrowth(t, before, f.r.trace) || len(f.r.data[f.prefix+"page"].hash["html"]) != MaxPageBlobBytes {
			t.Fatal("blob allocation bound or truncation")
		}
		keys, args := f.wire(t, OperationStagePageBlob, &chunk)
		encoded, _ := EncodeRecord(Record{textField("field_name", "html"), textField("field_bytes", strings.Repeat("x", MaxPageBlobBytes+1))})
		args[len(args)-1] = string(encoded)
		before = f.r.snapshot()
		got := stageOpsRun(t, f.r, stageOpsSource(t, OperationStagePageBlob), keys, args)
		if got.runtimeErr != nil || got.raw == nil {
			t.Fatal(got.runtimeErr)
		}
		if _, rejected := got.raw.(bootLuaErrorReply); !rejected || !reflect.DeepEqual(before, f.r.snapshot()) {
			t.Fatal("oversized blob admitted")
		}
	})
}

func TestStageOperationsTokenFreeCleanupProof(t *testing.T) {
	t.Parallel()
	f := stageOpsFixtureNew(t, 0, 0, 0)
	f.stageAll(t, false)
	f.expect(t, OperationCommit, nil, StatusCommitted)
	if f.r.data[f.jobKey].hash["lease_token"] != "" || f.r.data[f.jobKey].hash["lease_owner"] != "" {
		t.Fatal("cleanup fixture did not clear the published lease")
	}
	deadline := uint64(f.r.zsets[StageExpiryKey][string(f.commit)])
	gate := runLuaGate(t, f.a, OperationCleanStage, false)
	request, err := NewCleanStageWireRequest(gate, CleanStageWireInput{ExpectedCommitID: f.commit, ExpectedCleanupDueAtMS: deadline})
	keys, args := runLuaParts(t, request, err)
	if len(args) != 9 {
		t.Fatal("cleanup proof added a raw-token or other wire field")
	}
	probe := stageOpsCore(t) + `
local ctx,code=CJ.Context.open(CJ.Wire.maintenance_spec("CJ2_CLEAN_STAGE"),KEYS,ARGV)
if not ctx then return CJ.Context.reject(code) end
local gate,why=CJ.Gate.check(ctx); if not gate then return CJ.Context.reject(why) end
local view,err=CJ.Stage.select(ctx,ctx.request.v.expected_commit_id)
if not view then return CJ.Context.reject(err) end
local binding,failure=CJ.Context.bind_stage_owner(ctx)
if not binding then return CJ.Context.reject(failure) end
if binding.exists then
    local job,e=CJ.Read.fixed_hash(ctx,binding.job_key,"job")
    if not job then return CJ.Context.reject(e) end
end
local residue,e=CJ.Stage.check_cleanup_residue(view,ctx.request.v.expected_commit_id)
if not residue then return CJ.Context.reject(e) end
local forged=CJ.Stage.slot_proof(residue,ctx)
if forged then return CJ.Context.reject("INVALID_STATE") end
return {residue.kind,residue.deletion_only and "1" or "0",P.format_decimal(residue.key_count)}
`
	before := f.r.snapshot()
	got := sharedLuaNoError(t, stageOpsRun(t, f.r, probe, keys, args))
	if !reflect.DeepEqual(got, []any{"committed_residue", "1", "3"}) || f.r.attempts != 0 || !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("cleanup proof required cleared token, mutated, or conferred publication authority", got)
	}
	for _, field := range []string{"commit_id", "output_digest", "request_starts_generation"} {
		c := f.clone()
		c.r.data[c.prefix+"meta"].hash[field] = strings.Repeat("f", 64)
		result := stageOpsRun(t, c.r, probe, keys, args)
		if result.runtimeErr != nil {
			t.Fatal(result.runtimeErr)
		}
		if _, rejected := result.raw.(bootLuaErrorReply); !rejected || c.r.attempts != 0 {
			t.Fatal("changed retained cleanup identity accepted", field)
		}
	}
}

// This is a counter-only integration probe, NOT a replacement renewal handler.
// It uses the real RENEW decoder/gate/Run/Job/private reads and Plan. It NEVER
// writes a lease deadline, stage key, slot, activity timestamp or reservation.
// Worker owns invoking the new inspection before its real eligibility branch.
func stageRenewalInspectionProbe(t *testing.T, countRejection bool, omitStage bool, omitSlots bool) string {
	t.Helper()
	source := stageOpsCore(t) + `
local ok,code=CJ.Request.register_worker("CJ2_RENEW_LEASE")
if not ok then return CJ.Context.reject(code) end
local ctx,err=CJ.Context.open(CJ.Wire.worker_spec("CJ2_RENEW_LEASE"),KEYS,ARGV)
if not ctx then return CJ.Context.reject(err) end
local gate,why=CJ.Gate.check(ctx); if not gate then return CJ.Context.reject(why) end
local run,e=CJ.Run.load(ctx,ctx.request.v.run_id); if not run then return CJ.Context.reject(e) end
local raw,e=CJ.Read.fixed_hash(ctx,ctx.keys.job,"job"); if not raw then return CJ.Context.reject(e) end
local id=ctx.request.v.job_id
local view={ctx=ctx,job=raw}
for _,def in ipairs({{"jobs","set",10000},{"job_order","zset",10000},{"ready","zset",10000},
    {"ready_at","zset",10000},{"leased","zset",64},{"leased_at","zset",64},{"delayed","zset",10000},
    {"completed","zset",10000},{"dead","zset",10000},{"cancelled","zset",10000},
    {"commit_backpressure","zset",10},{"active_leases","zset",64}}) do
    local global=def[1]=="active_leases"
    local key=global and ctx.keys.active_leases or ctx.keys["run_"..def[1]]
    local member=global and run.run_id..":"..id or id
    local fact,failure=CJ.Read.members(ctx,key,def[2],{member},def[3],global and 97 or 64)
    if not fact then return CJ.Context.reject(failure) end
    view[def[1]]=fact
end
local job,e=CJ.Job.check(view,run,id); if not job then return CJ.Context.reject(e) end
`
	if !omitSlots {
		source += `local slots,e=CJ.Read.slots(ctx); if not slots then return CJ.Context.reject(e) end
`
	}
	if !omitStage {
		source += `
local commit=job.v.active_stage_commit_id
if commit=="" and job.v.last_stage_fence==job.v.lease_fence and job.v.last_stage_fence~="0" then commit=job.v.last_stage_commit_id end
if commit~="" then
    local bound,e=CJ.Context.bind_stage(ctx,id); if not bound then return CJ.Context.reject(e) end
    local selected,e=CJ.Stage.select(ctx,commit); if not selected then return CJ.Context.reject(e) end
end
`
	}
	source += `
-- Instrument only: the inspection must not ask Redis to fill a missing fact.
local redis_call=redis.call
redis.call=function() error("renewal inspection performed Redis I/O") end
local inspection,e=CJ.Stage.check_renewal_state(ctx,run,job)
redis.call=redis_call
if not inspection then return CJ.Context.reject(e) end
if inspection.kind~="renewal_inspection" or inspection.inspection_only~=true then return CJ.Context.reject("INVALID_STATE") end
local kind=inspection.kind
inspection.kind,inspection.remaining="owned",50331648
local spend=CJ.Stage.slot_proof(inspection,ctx)
local publish=CJ.Stage.publication_proof(inspection,ctx)
local begin=CJ.Stage.begin_proof(inspection,ctx)
local chunk=CJ.Stage.validate_chunk(ctx,run,job,inspection,ctx.request)
inspection.kind=kind
if spend or publish or begin or chunk then return CJ.Context.reject("INVALID_STATE") end
-- Live validators must still reject expired ownership. Inspection is not an
-- alternate route to any existing allocation/publication handle.
if job.n.lease_expires_at_ms<=ctx.now_ms and inspection.has_stage then
    local l={run_id=job.v.run_id,job_id=id,owner_id=job.v.lease_owner,lease_token=job.v.lease_token,fence=job.v.lease_fence}
    local live=CJ.Stage.check_owned({ctx=ctx},run,job,l,inspection.commit_id)
    local aborted=CJ.Stage.check_aborted({ctx=ctx},run,job,l,inspection.commit_id)
    if live or aborted then return CJ.Context.reject("INVALID_STATE") end
end
`
	if !countRejection {
		return source + `return {inspection.kind,inspection.state,inspection.has_stage and "1" or "0",
    inspection.expires_at_ms and P.format_decimal(inspection.expires_at_ms) or "none"}`
	}
	return source + `
if inspection.state=="aborted" then return CJ.Context.reject("INVALID_STATE") end
local a=ctx.request.v
if job.n.lease_expires_at_ms>ctx.now_ms and job.v.lease_owner==a.owner_id and job.v.lease_token==a.lease_token and job.v.lease_fence==a.fence then
    return CJ.Context.reject("INVALID_STATE") -- no actual renewal writes in this probe
end
local mutable,e=CJ.Run.mutable(ctx,run); if not mutable then return CJ.Context.reject(e) end
if run.n.finalized_at_ms~=0 then return CJ.Context.reject("INVALID_STATE") end
local plan,e=CJ.Plan.new(ctx); if not plan then return CJ.Context.reject(e) end
local delta,e=CJ.Run.plan_delta(ctx,run); if not delta then return CJ.Context.reject(e) end
local ok,e=CJ.Run.accumulate(delta,{run={renewal_rejections_total=1}}); if not ok then return CJ.Context.reject(e) end
local post,e=CJ.Run.flush(ctx,plan,delta); if not post then return CJ.Context.reject(e) end
local assessment,e=CJ.Plan.assess(ctx,plan); if not assessment then return CJ.Context.reject(e) end
local reply,e=CJ.Reply.build(ctx,"LEASE_LOST",{job.v.lease_fence}); if not reply then return CJ.Context.reject(e) end
local execution,e=CJ.Plan.seal(ctx,plan,assessment,reply); if not execution then return CJ.Context.reject(e) end
for i=1,execution.count do redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc)) end
return execution.reply
`
}

func stageRenewalWire(t *testing.T, f *stageOpsFixture, stale bool) ([]string, []string) {
	t.Helper()
	lease := f.lease
	if stale {
		lease.OwnerID, lease.Token, lease.Fence = OwnerID(strings.Repeat("b", 32)), LeaseToken(strings.Repeat("c", 64)), Fence(2)
	}
	request, err := NewRenewLeaseWireRequest(runLuaGate(t, f.a, OperationRenewLease, false), lease)
	return runLuaParts(t, request, err)
}

func stageRenewalLeaseExpiry(t *testing.T, f *stageOpsFixture) uint64 {
	t.Helper()
	n, err := strconv.ParseUint(f.r.data[f.jobKey].hash["lease_expires_at_ms"], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func stageRenewalAssertCounterOnly(t *testing.T, f *stageOpsFixture, before sharedLuaSnapshot, want uint64) {
	t.Helper()
	if f.r.attempts != 1 || f.r.aclCount != 1 {
		t.Fatal("inspection/counter probe attempted lease or other writes", f.r.attempts, f.r.aclCount)
	}
	for _, c := range f.r.trace {
		if !c.acl && stageOpsWrite(c.name) && (c.name != "HSET" || !reflect.DeepEqual(c.args, []string{f.runKey, "renewal_rejections_total", canonicalDecimal(want)})) {
			t.Fatal("unexpected renewal write", c.name)
		}
	}
	before.data[f.runKey].hash["renewal_rejections_total"] = canonicalDecimal(want)
	before.writes++
	if !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("counter updated lease/stage/slot/activity/retention or unrelated state")
	}
}

func TestStageRenewalInspectionExpiredAndStaleCounterOnly(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"owned", "expired", "none"} {
		for _, stale := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stale=%v", state, stale), func(t *testing.T) {
				f := stageOpsFixtureNew(t, 0, 0, 0)
				if state != "none" {
					f.expect(t, OperationBeginStage, nil, StatusStageBegun)
				}
				f.r.now = stageRenewalLeaseExpiry(t, f)
				expires := "none"
				if state != "none" {
					expires = f.r.data[f.prefix+"meta"].hash["expires_at_ms"]
				}
				if state == "expired" {
					f.r.now, _ = strconv.ParseUint(expires, 10, 64)
					// Model Redis logical expiry explicitly before snapshots. No
					// script mutation or repair is credited for passive expiration.
					for _, key := range wireOracleStageKeys(f.commit) {
						f.r.removeKey(key)
					}
				}
				keys, args := stageRenewalWire(t, f, stale)
				before := f.r.snapshot()
				got := sharedLuaNoError(t, stageOpsRun(t, f.r, stageRenewalInspectionProbe(t, false, false, false), keys, args))
				has := "1"
				if state == "none" {
					has = "0"
				}
				if !reflect.DeepEqual(got, []any{"renewal_inspection", state, has, expires}) || f.r.attempts != 0 || !reflect.DeepEqual(before, f.r.snapshot()) {
					t.Fatal("validation-only inspection changed state or used stale caller", got)
				}
				for count := uint64(1); count <= 2; count++ {
					before := f.r.snapshot()
					got := sharedLuaNoError(t, stageOpsRun(t, f.r, stageRenewalInspectionProbe(t, true, false, false), keys, args))
					if err := ValidateOperationResponse(OperationRenewLease, got); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, []any{"LEASE_LOST", canonicalDecimal(f.r.now), "1"}) {
						t.Fatal("wrong definitive rejection", got)
					}
					stageRenewalAssertCounterOnly(t, f, before, count)
				}
			})
		}
	}
	// A mismatched caller against an unexpired stored lease is the same inspection
	// path: metadata/token/commit identities are recomputed using STORED values.
	f := stageOpsFixtureNew(t, 0, 0, 0)
	f.expect(t, OperationBeginStage, nil, StatusStageBegun)
	liveKeys, liveArgs := stageRenewalWire(t, f, false)
	liveBefore := f.r.snapshot()
	liveInspection := sharedLuaNoError(t, stageOpsRun(t, f.r, stageRenewalInspectionProbe(t, false, false, false), liveKeys, liveArgs))
	if liveInspection.([]any)[1] != "owned" || f.r.attempts != 0 || !reflect.DeepEqual(liveBefore, f.r.snapshot()) {
		t.Fatal("unexpired stored-stage inspection was not validation-only")
	}
	keys, args := stageRenewalWire(t, f, true)
	before := f.r.snapshot()
	got := sharedLuaNoError(t, stageOpsRun(t, f.r, stageRenewalInspectionProbe(t, true, false, false), keys, args))
	if err := ValidateOperationResponse(OperationRenewLease, got); err != nil {
		t.Fatal(err)
	}
	stageRenewalAssertCounterOnly(t, f, before, 1)
}

func TestStageRenewalInspectionCorruptionNeverCounts(t *testing.T) {
	t.Parallel()
	base := stageOpsFixtureNew(t, 0, 0, 0)
	base.stageAll(t, false)
	base.r.now = stageRenewalLeaseExpiry(t, base)
	for _, mutation := range []string{"B", "G", "run", "job", "owner", "fence", "token", "commit", "publication", "output",
		"abandoned", "slot-run", "slot-job", "slot-fence", "slot-aborted", "slot-missing", "duplicate-slot", "expiry", "expiry-missing",
		"missing-meta", "inventory", "ttl-extended", "ttl-persistent", "wrong-page-source", "deadline-beyond-stage"} {
		t.Run(mutation, func(t *testing.T) {
			f := base.clone()
			meta := f.r.data[f.prefix+"meta"].hash
			code := ErrorCode("STAGE_INVALID")
			slot := strings.Split(f.r.data[StageSlotsKey].hash[string(f.commit)], ":")
			switch mutation {
			case "B":
				meta["request_starts_baseline"] = "1"
				code = ErrorCode("COUNTER_CORRUPT")
			case "G":
				meta["request_starts_generation"] = "2"
				code = ErrorCode("COUNTER_CORRUPT")
			case "run", "job", "owner", "commit", "publication":
				name := mutation + "_id"
				width := 64
				if mutation == "run" || mutation == "owner" {
					width = 32
				}
				meta[name] = strings.Repeat("a", width)
			case "fence":
				meta["lease_fence"] = "2"
			case "token":
				meta["token_digest"] = strings.Repeat("a", 64)
			case "output":
				meta["output_digest"] = strings.Repeat("a", 64)
			case "abandoned":
				meta["abandoned"] = "1"
			case "slot-run":
				slot[1] = strings.Repeat("a", 32)
			case "slot-job":
				slot[2] = strings.Repeat("a", 64)
			case "slot-fence":
				slot[3] = "2"
			case "slot-aborted":
				slot[4] = "5"
			case "duplicate-slot":
				f.r.data[StageSlotsKey].hash[strings.Repeat("a", 64)] = strings.Join(slot, ":")
			case "expiry":
				f.r.zsets[StageExpiryKey][string(f.commit)]--
			case "expiry-missing":
				f.r.removeKey(StageExpiryKey)
			case "inventory":
				f.r.lists[f.prefix+"keys"][0] = f.prefix + "../not-authority"
			case "missing-meta":
				f.r.removeKey(f.prefix + "meta")
			case "ttl-extended", "ttl-persistent":
				e := f.r.data[f.prefix+"page"]
				e.expireAt++
				if mutation == "ttl-persistent" {
					e.expireAt = -1
				}
				f.r.data[f.prefix+"page"] = e
			case "wrong-page-source":
				f.r.data[f.prefix+"page"].hash["normalized_url"] = "https://example.com/other"
			case "deadline-beyond-stage":
				at := uint64(f.r.zsets[StageExpiryKey][string(f.commit)]) + 1
				f.r.data[f.jobKey].hash["lease_expires_at_ms"] = canonicalDecimal(at)
				f.r.zsets[f.runKey+":leased"][string(f.lease.JobID)] = float64(at)
				f.r.zsets[ActiveLeasesKey][runLuaID+":"+string(f.lease.JobID)] = float64(at)
			}
			f.r.data[StageSlotsKey].hash[string(f.commit)] = strings.Join(slot, ":")
			if mutation == "slot-missing" {
				f.r.removeKey(StageSlotsKey)
			}
			keys, args := stageRenewalWire(t, f, true)
			before := f.r.snapshot()
			got := stageOpsRun(t, f.r, stageRenewalInspectionProbe(t, true, false, false), keys, args)
			if got.runtimeErr != nil || got.raw != bootLuaErrorReply("ERR CRAWL_V2_"+string(code)) {
				t.Fatalf("%s: %v / %v", mutation, got.raw, got.runtimeErr)
			}
			if f.r.attempts != 0 || !reflect.DeepEqual(before, f.r.snapshot()) {
				t.Fatal("corrupt inspection counted a rejection or changed state")
			}
		})
	}
}

func TestStageRenewalInspectionExpiredAbsenceAndAbortBoundaries(t *testing.T) {
	t.Parallel()
	base := stageOpsFixtureNew(t, 0, 0, 0)
	base.expect(t, OperationBeginStage, nil, StatusStageBegun)
	due := uint64(base.r.zsets[StageExpiryKey][string(base.commit)])
	for _, which := range []string{"premature-absence", "expired-partial", "missing-expiry", "too-early-expiry", "lease-beyond-expiry"} {
		t.Run(which, func(t *testing.T) {
			f := base.clone()
			for _, key := range wireOracleStageKeys(f.commit) {
				f.r.removeKey(key)
			}
			f.r.now = due
			switch which {
			case "premature-absence":
				f.r.now = due - 1
			case "expired-partial":
				f.r.setHash(f.prefix+"page", Record{textField("html", "must-not-survive")})
				e := f.r.data[f.prefix+"page"]
				e.expireAt = int64(due + 1)
				f.r.data[f.prefix+"page"] = e
			case "missing-expiry":
				f.r.removeKey(StageExpiryKey)
			case "too-early-expiry":
				f.r.zsets[StageExpiryKey][string(f.commit)] = float64(stageRenewalLeaseExpiry(t, f))
			case "lease-beyond-expiry":
				f.r.data[f.jobKey].hash["lease_expires_at_ms"] = canonicalDecimal(due + 1)
				f.r.zsets[f.runKey+":leased"][string(f.lease.JobID)] = float64(due + 1)
				f.r.zsets[ActiveLeasesKey][runLuaID+":"+string(f.lease.JobID)] = float64(due + 1)
				f.r.now++
			}
			keys, args := stageRenewalWire(t, f, true)
			before := f.r.snapshot()
			got := stageOpsRun(t, f.r, stageRenewalInspectionProbe(t, true, false, false), keys, args)
			if got.runtimeErr != nil || got.raw != bootLuaErrorReply("ERR CRAWL_V2_STAGE_INVALID") || f.r.attempts != 0 || !reflect.DeepEqual(before, f.r.snapshot()) {
				t.Fatal("invalid expiry proof admitted/counts", which, got.raw, got.runtimeErr)
			}
		})
	}
	aborted := base.clone()
	aborted.expect(t, OperationAbortStage, nil, StatusStageAborted)
	for _, expired := range []bool{false, true} {
		for _, stale := range []bool{false, true} {
			f := aborted.clone()
			if expired {
				f.r.now = stageRenewalLeaseExpiry(t, f)
			}
			keys, args := stageRenewalWire(t, f, stale)
			before := f.r.snapshot()
			got := sharedLuaNoError(t, stageOpsRun(t, f.r, stageRenewalInspectionProbe(t, false, false, false), keys, args))
			if !reflect.DeepEqual(got, []any{"renewal_inspection", "aborted", "1", "none"}) {
				t.Fatal("aborted inspection depended on caller or lease eligibility", got)
			}
			result := stageOpsRun(t, f.r, stageRenewalInspectionProbe(t, true, false, false), keys, args)
			if result.runtimeErr != nil || result.raw != bootLuaErrorReply("ERR CRAWL_V2_INVALID_STATE") || f.r.attempts != 0 || !reflect.DeepEqual(before, f.r.snapshot()) {
				t.Fatal("aborted terminal slot authorized renewal/counter", result.raw, result.runtimeErr)
			}
		}
	}
	for _, corruption := range []string{"expiry-member", "resurrected-key", "count-one", "owner-slot"} {
		f := aborted.clone()
		f.r.now = stageRenewalLeaseExpiry(t, f)
		slot := strings.Split(f.r.data[StageSlotsKey].hash[string(f.commit)], ":")
		switch corruption {
		case "expiry-member":
			f.r.setZSet(StageExpiryKey, map[string]float64{string(f.commit): float64(due)})
		case "resurrected-key":
			f.r.setList(f.prefix+"keys", []string{f.prefix + "meta", f.prefix + "keys"})
		case "count-one":
			slot[4] = "1"
		case "owner-slot":
			slot[4] = "0"
		}
		f.r.data[StageSlotsKey].hash[string(f.commit)] = strings.Join(slot, ":")
		keys, args := stageRenewalWire(t, f, true)
		before := f.r.snapshot()
		result := stageOpsRun(t, f.r, stageRenewalInspectionProbe(t, true, false, false), keys, args)
		if result.runtimeErr != nil || result.raw != bootLuaErrorReply("ERR CRAWL_V2_STAGE_INVALID") || f.r.attempts != 0 || !reflect.DeepEqual(before, f.r.snapshot()) {
			t.Fatal("corrupt aborted slot counted", corruption, result.raw, result.runtimeErr)
		}
	}
}

func TestStageRenewalInspectionRequiresExplicitReceiptsAndRenewContext(t *testing.T) {
	t.Parallel()
	for _, staged := range []bool{false, true} {
		f := stageOpsFixtureNew(t, 0, 0, 0)
		if staged {
			f.expect(t, OperationBeginStage, nil, StatusStageBegun)
		}
		f.r.now = stageRenewalLeaseExpiry(t, f)
		keys, args := stageRenewalWire(t, f, true)
		before := f.r.snapshot()
		got := stageOpsRun(t, f.r, stageRenewalInspectionProbe(t, true, true, !staged), keys, args)
		if got.runtimeErr != nil || got.raw != bootLuaErrorReply("ERR CRAWL_V2_INVALID_STATE") || f.r.attempts != 0 || !reflect.DeepEqual(before, f.r.snapshot()) {
			t.Fatal("missing receipt became zero/absence", got.raw, got.runtimeErr)
		}
	}
	f := stageOpsFixtureNew(t, 0, 0, 0)
	keys, args := f.wire(t, OperationBeginStage, nil)
	probe := stageOpsCore(t) + `
local ctx,code=CJ.Context.open(CJ.Wire.stage_spec("CJ2_BEGIN_STAGE"),KEYS,ARGV)
if not ctx then return CJ.Context.reject(code) end
local result,why=CJ.Stage.check_renewal_state(ctx,{},{});
if result then return "UNEXPECTED" end
return CJ.Context.reject(why)`
	before := f.r.snapshot()
	got := stageOpsRun(t, f.r, probe, keys, args)
	if got.runtimeErr != nil || got.raw != bootLuaErrorReply("ERR CRAWL_V2_INVALID_STATE") || f.r.attempts != 0 || !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("inspection was not closed to RENEW", got.raw, got.runtimeErr)
	}
}
