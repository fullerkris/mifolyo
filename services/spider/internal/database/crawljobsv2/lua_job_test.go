package crawljobsv2

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
	lua "github.com/yuin/gopher-lua"
)

// These tests execute the actual pure chunks. Go supplies fixtures/oracles,
// never replacements for schema, URL, SHA, framing, or policy logic.
type jobLuaVM struct {
	*urlLuaVM
	cj, env *lua.LTable
}

func jobLuaNew(t *testing.T) *jobLuaVM {
	t.Helper()
	if runtime.Version() != "go1.25.13" {
		t.Fatal("job differential oracle requires Go 1.25.13")
	}
	u := urlLuaNew(t, false)
	l := u.state
	env := l.NewTable()
	for _, name := range []string{"type", "next", "rawget", "getmetatable", "ipairs", "pairs", "unpack", "tonumber", "pcall"} {
		env.RawSetString(name, l.GetGlobal(name))
	}
	for _, name := range []string{"math", "string", "table"} {
		env.RawSetString(name, l.GetGlobal(name))
	}
	cj := l.NewTable()
	cj.RawSetString("P", u.module)
	cj.RawSetString("URL", u.url)
	vm := &jobLuaVM{urlLuaVM: u, cj: cj, env: env}
	for _, m := range []struct{ name, path string }{
		{"Identities", "identities"}, {"Schemas", "schemas"}, {"Wire", "wire"}, {"Context", "context"},
		{"Read", "read"}, {"Plan", "plan"}, {"Run", "ledger_run"}, {"Job", "ledger_job"},
	} {
		vm.load(t, m.name, m.path)
	}
	return vm
}

func (vm *jobLuaVM) load(t *testing.T, name, path string) {
	t.Helper()
	source := "return function(P,CJ)\n" + string(primitiveLuaRead(t, "lua_src/"+path+".lua")) + "\nend"
	fn, err := vm.state.Load(strings.NewReader(source), path+" factory")
	if err != nil {
		t.Fatal(err)
	}
	vm.state.SetFEnv(fn, vm.env)
	if err := vm.state.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}); err != nil {
		t.Fatal(err)
	}
	factory := vm.state.Get(-1)
	vm.state.Pop(1)
	module, code := primitiveLuaInvoke(t, vm.primitiveLuaVM, factory, vm.module, vm.cj)
	if _, ok := module.(*lua.LTable); !ok || code != lua.LNil {
		t.Fatalf("load %s: %v / %v", path, module, code)
	}
	vm.cj.RawSetString(name, module)
}

func (vm *jobLuaVM) invoke(t *testing.T, module, method string, args ...lua.LValue) (lua.LValue, lua.LValue) {
	t.Helper()
	return primitiveLuaInvoke(t, vm.primitiveLuaVM, vm.cj.RawGetString(module).(*lua.LTable).RawGetString(method), args...)
}

func jobLuaValues(vm *jobLuaVM, record Record) *lua.LTable {
	v := vm.state.NewTable()
	for _, f := range record {
		v.RawSetString(f.Name, lua.LString(f.Value))
	}
	return v
}

func jobLuaCompare(t *testing.T, vm *jobLuaVM, record Record) bool {
	t.Helper()
	want := ValidateRecord(SchemaJob, record)
	encoded, err := EncodeRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	got, code := vm.invoke(t, "Schemas", "decode", lua.LString("job"), lua.LString(encoded))
	if (got != lua.LNil) != (want == nil) {
		projected, projectCode := vm.invoke(t, "Schemas", "project", lua.LString("job"), jobLuaValues(vm, record))
		t.Fatalf("Lua=%v/%v project=%v/%v Go=%v record=%v", got, code, projected, projectCode, want, record)
	}
	if got == lua.LNil {
		if _, err := ParseErrorCode(code.String()); err != nil {
			t.Fatalf("non-closed rejection %v", code)
		}
		return false
	}
	if code != lua.LNil {
		t.Fatalf("success and error: %v", code)
	}
	// The core numeric projection contains every successful canonical uint parse,
	// not just the schema's counters. No missing required numeric field is zeroed.
	numbers := got.(*lua.LTable).RawGetString("n").(*lua.LTable)
	for _, f := range record {
		decimal, err := ParseUnsignedDecimal(string(f.Value))
		if err != nil {
			if numbers.RawGetString(f.Name) != lua.LNil {
				t.Fatalf("noncanonical numeric projection for %s", f.Name)
			}
			continue
		}
		n, _ := decimal.Uint64()
		if numbers.RawGetString(f.Name) != lua.LNumber(n) {
			t.Fatalf("numeric projection omitted/changed %s", f.Name)
		}
	}
	roundtrip, failure := vm.invoke(t, "Schemas", "encode", got)
	if failure != lua.LNil || roundtrip != lua.LString(encoded) {
		t.Fatalf("projection lost a field/witness: %v/%v", roundtrip, failure)
	}
	return true
}

func jobLuaFixtures(t *testing.T) map[string]Record {
	t.Helper()
	result := map[string]Record{}
	for _, state := range []string{"ready", "leased", "delayed", "completed", "dead", "cancelled"} {
		result[state] = recordAuthorityJobRecord(t, state)
	}
	result["published"] = recordAuthorityPublishedJobRecord(t)
	result["aborted"], _, _ = reviewPostAbortBackpressureJob(t)
	result["retry_exhausted"] = reviewRetryExhaustedJob(t)
	result["pre_io_exhausted"] = reviewPreIOExhaustedJob(t)
	staged := cloneRecord(result["aborted"])
	recordAuthoritySet(staged, jobActiveStageCommitIDIndex, string(staged[jobLastStageCommitIDIndex].Value))
	reviewSetJobTransition(staged, Digest(strings.Repeat("b", 64)), StatusStageBegun)
	result["staged"] = staged
	preIO := cloneRecord(result["leased"])
	for _, index := range []int{jobDeliveryAttemptsIndex, jobRequestStartsIndex, jobLastRequestStartedAtMSIndex, jobLeaseDeliveryStartedIndex} {
		recordAuthoritySet(preIO, index, "0")
	}
	result["pre_io"] = preIO
	return result
}

func TestJobLuaAllStatesAndEveryField(t *testing.T) {
	t.Parallel()
	vm := jobLuaNew(t)
	definition, code := vm.invoke(t, "Schemas", "get", lua.LString("job"))
	if code != lua.LNil {
		t.Fatal(code)
	}
	d := definition.(*lua.LTable)
	for i, f := range recordAuthorityJobRecord(t, "ready") {
		if d.RawGetString("names").(*lua.LTable).RawGetInt(i+1) != lua.LString(f.Name) ||
			int(d.RawGetString("bounds").(*lua.LTable).RawGetInt(i+1).(lua.LNumber)) < len(f.Value) {
			t.Fatalf("definition field %d %s", i, f.Name)
		}
	}
	for name, record := range jobLuaFixtures(t) {
		t.Run(name, func(t *testing.T) {
			if len(record) != 54 || !jobLuaCompare(t, vm, record) {
				t.Fatal("independent valid baseline rejected")
			}
			for i, field := range record {
				for _, value := range []string{"", "0", "00", "not-a-value", "\xff", "\x00"} {
					candidate := cloneRecord(record)
					recordAuthoritySet(candidate, i, value)
					t.Run(field.Name+"/"+fmt.Sprintf("%x", value), func(t *testing.T) { jobLuaCompare(t, vm, candidate) })
				}
				missing := append(cloneRecord(record[:i]), cloneRecord(record[i+1:])...)
				jobLuaCompare(t, vm, missing)
				wrongName := cloneRecord(record)
				wrongName[i].Name += "_unexpected"
				jobLuaCompare(t, vm, wrongName)
			}
		})
	}
}

func TestJobLuaBGHistoryAndRetainedFreeze(t *testing.T) {
	t.Parallel()
	vm := jobLuaNew(t)
	for b := 0; b <= 10; b++ {
		for g := 0; g <= 10; g++ {
			for attempts := 0; attempts <= 3; attempts++ {
				job := recordAuthorityJobRecord(t, "leased")
				for index, value := range map[int]int{
					jobClaimCountIndex: 3, jobLeaseFenceIndex: 3, jobNextRequestOrdinalIndex: 12,
					jobLeaseRequestStartsBaselineIndex: b, jobRequestStartsIndex: g, jobDeliveryAttemptsIndex: attempts,
				} {
					recordAuthoritySet(job, index, strconv.Itoa(value))
				}
				if g == 0 {
					recordAuthoritySet(job, jobLastRequestStartedAtMSIndex, "0")
				}
				if g <= b {
					recordAuthoritySet(job, jobLeaseDeliveryStartedIndex, "0")
				}
				jobLuaCompare(t, vm, job)
				reviewClearLeaseAndBackpressure(job)
				recordAuthoritySet(job, jobStateIndex, "cancelled")
				recordAuthoritySet(job, jobLastReasonIndex, "operator_cancelled")
				recordAuthoritySet(job, jobCancelledAtMSIndex, "200")
				jobLuaCompare(t, vm, job)
			}
		}
	}
	aborted, _, _ := reviewPostAbortBackpressureJob(t)
	for _, index := range []int{jobLastTransitionIDIndex, jobLastStageCommitIDIndex, jobLeaseTokenIndex, jobLeaseOwnerIndex,
		jobLastDocumentRequestFenceIndex, jobLeaseRequestStartsBaselineIndex, jobActiveReservationIDIndex} {
		candidate := cloneRecord(aborted)
		recordAuthoritySet(candidate, index, strings.Repeat("b", 64))
		if jobLuaCompare(t, vm, candidate) {
			t.Fatalf("aborted witness mutation %d accepted", index)
		}
	}
	// A later pre-I/O fence may retain an older document and stage. No comparison
	// to wall-clock now, nor clearing retained B/G/doc fields, is legal here.
	for _, index := range []int{jobCommitBackpressureFenceIndex, jobCommitBackpressureStartedAtMSIndex, jobCommitBackpressureDeadlineMSIndex} {
		recordAuthoritySet(aborted, index, "0")
	}
	recordAuthoritySet(aborted, jobCommitBackpressureReasonIndex, "none")
	for index, value := range map[int]string{jobClaimCountIndex: "2", jobLeaseFenceIndex: "2", jobLeaseRequestStartsBaselineIndex: "1",
		jobLeaseDeliveryStartedIndex: "0", jobNextRequestOrdinalIndex: "3"} {
		recordAuthoritySet(aborted, index, value)
	}
	reviewSetJobTransition(aborted, Digest(strings.Repeat("b", 64)), StatusClaimed)
	if !jobLuaCompare(t, vm, aborted) {
		t.Fatal("older witnesses are not invalid current-lease evidence")
	}
}

func TestJobLuaURLLayersAndPublication(t *testing.T) {
	t.Parallel()
	vm := jobLuaNew(t)
	for _, url := range []string{"https://xn--bcher-kva.example/", "https://example.com/a%2Fb?x=%FF", "http://127.0.0.1/",
		"https://[2001:db8::1]/", "https://bücher.example/", "https://xn--a.example/", "HTTPS://example.com/", "https://example.com:443/"} {
		job := recordAuthorityJobRecord(t, "ready")
		recordAuthoritySet(job, jobCanonicalURLIndex, url)
		recordAuthoritySet(job, jobJobIDIndex, utils.URLIDV1(url))
		recordAuthoritySet(job, jobURLIDIndex, utils.URLIDV1(url))
		if origin, err := DeriveCanonicalOrigin(url); err == nil {
			scope, _ := DeriveOriginScopeID(origin)
			recordAuthoritySet(job, jobInitialOriginScopeIDIndex, string(scope))
		}
		jobLuaCompare(t, vm, job)
		// Unlike source admission, the isolated document-target identity validator
		// must not invent a DNS-origin policy constraint absent from that witness.
		job = recordAuthorityPublishedJobRecord(t)
		recordAuthoritySet(job, jobLastDocumentTargetURLIndex, url)
		recordAuthoritySet(job, jobLastDocumentTargetURLIDIndex, utils.URLIDV1(url))
		target, err := DeriveTargetDigest(RequestTarget{URLID: JobID(utils.URLIDV1(url)), CanonicalURL: url})
		if err == nil {
			recordAuthoritySet(job, jobLastDocumentTargetDigestIndex, string(target))
			pageKey, err := PageDataKey(Digest(job[jobPublicationIDIndex].Value), url)
			if err != nil {
				t.Fatal(err)
			}
			recordAuthoritySet(job, jobPublishedPageKeyIndex, pageKey)
		}
		jobLuaCompare(t, vm, job)
	}
	published := recordAuthorityPublishedJobRecord(t)
	for _, index := range []int{jobPublicationIDIndex, jobOutputDigestIndex, jobCommitIDIndex, jobPublishedPageKeyIndex} {
		job := cloneRecord(published)
		recordAuthoritySet(job, index, strings.Repeat("e", 64))
		if jobLuaCompare(t, vm, job) {
			t.Fatalf("publication relation %d accepted", index)
		}
	}
}

func TestJobLuaNumbersReasonsAndBackpressure(t *testing.T) {
	t.Parallel()
	vm := jobLuaNew(t)
	fixtures := jobLuaFixtures(t)
	for _, s := range []string{"-1000", "10000", "-999.999999", "9999.999999", "0.000001", "-0.000001", "-0", "1.0", "01",
		"+1", "1e2", "nan", "inf", "10000.000001", "-1000.000001", "0.0000001", "1\n"} {
		job := cloneRecord(fixtures["ready"])
		recordAuthoritySet(job, jobScoreTextIndex, s)
		jobLuaCompare(t, vm, job)
	}
	for _, index := range []int{jobDepthIndex, jobClaimCountIndex, jobDeliveryAttemptsIndex, jobRequestStartsIndex,
		jobLeaseRequestStartsBaselineIndex, jobRetryCountIndex, jobPreIORecoveriesIndex, jobNextRequestOrdinalIndex,
		jobLeaseFenceIndex, jobCreatedAtMSIndex, jobUpdatedAtMSIndex} {
		for _, value := range []string{"3", "10", "100", "101", "102", "9007199254740991", "9007199254740992", "1e2", "-1"} {
			job := cloneRecord(fixtures["leased"])
			recordAuthoritySet(job, index, value)
			jobLuaCompare(t, vm, job)
		}
	}
	for _, name := range []string{"staged", "aborted"} {
		for _, deadline := range []string{"0", "1", "149", "150", "120150", "120151"} {
			job := cloneRecord(fixtures[name])
			recordAuthoritySet(job, jobCommitBackpressureDeadlineMSIndex, deadline)
			jobLuaCompare(t, vm, job)
		}
	}
	for _, record := range fixtures {
		for reason := range reasons {
			for _, index := range []int{jobLastReasonIndex, jobLastFailureReasonIndex} {
				job := cloneRecord(record)
				recordAuthoritySet(job, index, string(reason))
				jobLuaCompare(t, vm, job)
			}
		}
		for status := range statuses {
			job := cloneRecord(record)
			reviewSetJobTransition(job, Digest(strings.Repeat("b", 64)), status)
			jobLuaCompare(t, vm, job)
		}
	}
}

func TestJobLuaSchemaBoundsAndWitnessSentinels(t *testing.T) {
	t.Parallel()
	vm := jobLuaNew(t)
	definition, code := vm.invoke(t, "Schemas", "get", lua.LString("job"))
	if code != lua.LNil {
		t.Fatal(code)
	}
	d := definition.(*lua.LTable)
	names, bounds := d.RawGetString("names").(*lua.LTable), d.RawGetString("bounds").(*lua.LTable)
	if names.Len() != 54 || bounds.Len() != 54 {
		t.Fatal("schema is not exactly 54 fields")
	}
	ready := recordAuthorityJobRecord(t, "ready")
	maximum := 8
	for i, field := range ready {
		bound := int(bounds.RawGetInt(i + 1).(lua.LNumber))
		maximum += 16 + len(field.Name) + bound
		candidate := cloneRecord(ready)
		recordAuthoritySet(candidate, i, strings.Repeat("a", bound+1))
		if jobLuaCompare(t, vm, candidate) {
			t.Fatalf("over-bound %s accepted", field.Name)
		}
		// All-zero IDs are legal hex; all-zero digests are not. Let Go decide
		// rather than applying a blanket "nonzero identity" rule in Lua.
		for _, size := range []int{32, 64} {
			recordAuthoritySet(candidate, i, strings.Repeat("0", size))
			jobLuaCompare(t, vm, candidate)
		}
	}
	if d.RawGetString("maximum") != lua.LNumber(maximum) {
		t.Fatal("framed RECORD bound drift")
	}
	for _, record := range []Record{append(cloneRecord(ready), textField("unexpected", "")), cloneRecord(ready)} {
		record[0], record[1] = record[1], record[0]
		if jobLuaCompare(t, vm, record) {
			t.Fatal("extra/reordered fields accepted")
		}
	}
	// The maximum published queue key is 2806 bytes, including the unpadded
	// base64url of a 2048-byte target. Exercise all three final-block lengths.
	for _, length := range []int{2046, 2047, 2048} {
		url := "https://example.com/" + strings.Repeat("x", length-len("https://example.com/"))
		published := recordAuthorityPublishedJobRecord(t)
		target := RequestTarget{URLID: JobID(utils.URLIDV1(url)), CanonicalURL: url}
		digest, err := DeriveTargetDigest(target)
		if err != nil {
			t.Fatal(err)
		}
		key, err := PageDataKey(Digest(published[jobPublicationIDIndex].Value), url)
		if err != nil {
			t.Fatal(err)
		}
		for index, value := range map[int]string{jobLastDocumentTargetURLIndex: url, jobLastDocumentTargetURLIDIndex: string(target.URLID),
			jobLastDocumentTargetDigestIndex: string(digest), jobPublishedPageKeyIndex: key} {
			recordAuthoritySet(published, index, value)
		}
		if !jobLuaCompare(t, vm, published) {
			t.Fatal("valid maximum publication witness rejected")
		}
		if length == 2048 && len(key) != 2806 {
			t.Fatal("page key bound no longer matches Go authority")
		}
	}
	staged := jobLuaFixtures(t)["staged"]
	for mask := 0; mask < 32; mask++ {
		candidate := cloneRecord(staged)
		for bit, index := range []int{jobLastDocumentRequestStartedAtMSIndex, jobLastDocumentRequestFenceIndex,
			jobLastDocumentTargetURLIDIndex, jobLastDocumentTargetURLIndex, jobLastDocumentTargetDigestIndex} {
			if mask&(1<<bit) != 0 {
				value := ""
				if bit < 2 {
					value = "0"
				}
				recordAuthoritySet(candidate, index, value)
			}
		}
		jobLuaCompare(t, vm, candidate)
	}
}

func TestJobLuaAbortIdentityEveryInput(t *testing.T) {
	t.Parallel()
	vm := jobLuaNew(t)
	baseline, _, _ := reviewPostAbortBackpressureJob(t)
	for _, field := range []string{"run", "job", "owner", "token", "fence", "commit"} {
		t.Run(field, func(t *testing.T) {
			record := cloneRecord(baseline)
			switch field {
			case "run":
				recordAuthoritySet(record, jobRunIDIndex, strings.Repeat("0", 32))
			case "job":
				source, err := completeSourceJobRecord(jobLuaSourceValue(t, "https://example.com/another"))
				if err != nil {
					t.Fatal(err)
				}
				jobLuaApplySource(record, source)
			case "owner":
				recordAuthoritySet(record, jobLeaseOwnerIndex, strings.Repeat("0", 32))
			case "token":
				recordAuthoritySet(record, jobLeaseTokenIndex, strings.Repeat("0", 64))
			case "fence":
				for _, index := range []int{jobClaimCountIndex, jobLeaseFenceIndex, jobLastStageFenceIndex,
					jobLastDocumentRequestFenceIndex, jobCommitBackpressureFenceIndex} {
					recordAuthoritySet(record, index, "2")
				}
				recordAuthoritySet(record, jobNextRequestOrdinalIndex, "3")
			case "commit":
				recordAuthoritySet(record, jobLastStageCommitIDIndex, strings.Repeat("c", 64))
			}
			if jobLuaCompare(t, vm, record) {
				t.Fatal("old abort witness accepted")
			}
			fence, err := ParseFence(string(record[jobLeaseFenceIndex].Value))
			if err != nil {
				t.Fatal(err)
			}
			transition, err := DeriveAbortStageTransitionID(AbortStageTransitionInput{
				Lease: LeaseIdentity{RunID: RunID(record[jobRunIDIndex].Value), JobID: JobID(record[jobJobIDIndex].Value),
					OwnerID: OwnerID(record[jobLeaseOwnerIndex].Value), Token: LeaseToken(record[jobLeaseTokenIndex].Value), Fence: fence},
				CommitID: Digest(record[jobLastStageCommitIDIndex].Value),
			})
			if err != nil {
				t.Fatal(err)
			}
			reviewSetJobTransition(record, transition, StatusStageAborted)
			if !jobLuaCompare(t, vm, record) {
				t.Fatal("Go-rebound exact abort witness rejected")
			}
		})
	}
}

// Keep independent byte/numeric comparison helpers local to this file's unique
// namespace; future agents can add their own harnesses without collisions.
func jobLuaAssertRecord(t *testing.T, projection lua.LValue, want Record) {
	t.Helper()
	p := projection.(*lua.LTable)
	fields := p.RawGetString("fields").(*lua.LTable)
	if fields.Len() != len(want) {
		t.Fatalf("fields: %d want %d", fields.Len(), len(want))
	}
	for i, f := range want {
		field := fields.RawGetInt(i + 1).(*lua.LTable)
		if field.RawGetInt(1) != lua.LString(f.Name) || !bytes.Equal([]byte(field.RawGetInt(2).String()), f.Value) {
			t.Fatalf("field %d changed", i)
		}
	}
}

func jobLuaSourceValue(t *testing.T, url string) SourceJob {
	t.Helper()
	decision, err := NewPolicyDecision(PolicyDecisionInput{RequestKind: RequestDocument,
		Target: RequestTarget{URLID: JobID(utils.URLIDV1(url)), CanonicalURL: url}, Depth: 0,
		GroupID: "default", RateScopeID: RateScopeID(strings.Repeat("2", 32)),
		GroupConcurrency: 3, OriginConcurrency: 3, GroupIntervalMS: 100, OriginIntervalMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	return SourceJob{JobID: decision.TargetURLID, CanonicalURL: url, ScoreText: "0", Depth: 0,
		GroupID: decision.GroupID, RateScopeID: decision.RateScopeID, Decision: decision}
}

func jobLuaGroup(source SourceJob) PolicyGroup {
	return PolicyGroup{GroupID: source.GroupID, RateScopeID: source.RateScopeID, GroupScopeID: source.Decision.GroupScopeID,
		RequestStartLimit: 10, Concurrency: source.Decision.GroupConcurrency, IntervalMS: source.Decision.GroupIntervalMS}
}

func jobLuaBinding(t *testing.T, vm *jobLuaVM, groups []PolicyGroup) (*lua.LTable, Record) {
	t.Helper()
	array := vm.state.NewTable()
	for i, g := range groups {
		r, err := policyGroupRecord(g)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := EncodeRecord(r)
		array.RawSetInt(i+1, lua.LString(b))
	}
	projection, code := vm.invoke(t, "Schemas", "groups", array)
	if code != lua.LNil {
		t.Fatal(code)
	}
	run := recordAuthorityRunRecord(t, "active")
	digest, err := DerivePolicyGroupMapDigest(groups)
	if err != nil {
		t.Fatal(err)
	}
	recordAuthoritySet(run, runPolicyGroupMapSHA256Index, string(digest))
	recordAuthoritySet(run, runPolicyGroupCountIndex, strconv.Itoa(len(groups)))
	if err := ValidateRecord(SchemaRun, run); err != nil {
		t.Fatal(err)
	}
	ledger := vm.state.NewTable()
	ledger.RawSetString("run_id", lua.LString(strings.Repeat("1", 32)))
	ledger.RawSetString("v", jobLuaValues(vm, run))
	ledger.RawSetString("groups", projection)
	return ledger, run
}

func jobLuaEncoded(t *testing.T, record Record) lua.LString {
	t.Helper()
	b, err := EncodeRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	return lua.LString(b)
}

// This oracle does not reproduce a digest formula. It reconstructs the typed
// source with Go's parsers/NewPolicyDecision, validates actual authenticated run
// authority, then requires byte equality with the production ENQUEUE encoder.
func jobLuaSourceOracle(authority RunPolicyAuthority, record Record) error {
	if len(record) != 9 {
		return ErrInvalidSchema
	}
	v, err := strictTextValues(record)
	if err != nil {
		return err
	}
	depth, err := ParseUnsignedDecimal(v[3])
	if err != nil {
		return err
	}
	n, _ := depth.Uint64()
	_, groups, err := authenticatedRunPolicyGroupMap(authority)
	if err != nil {
		return err
	}
	group, ok := groups[GroupID(v[4])]
	if !ok {
		return ErrPolicyGroupBindingMismatch
	}
	decision, err := NewPolicyDecision(PolicyDecisionInput{RequestKind: RequestDocument,
		Target: RequestTarget{URLID: JobID(v[0]), CanonicalURL: v[1]}, Depth: n,
		GroupID: GroupID(v[4]), RateScopeID: RateScopeID(v[5]), GroupConcurrency: group.Concurrency,
		OriginConcurrency: group.Concurrency, GroupIntervalMS: group.IntervalMS, OriginIntervalMS: group.IntervalMS})
	if err != nil {
		return err
	}
	source := SourceJob{JobID: JobID(v[0]), CanonicalURL: v[1], ScoreText: ScoreText(v[2]), Depth: n,
		GroupID: GroupID(v[4]), RateScopeID: RateScopeID(v[5]), Decision: decision}
	if err := validateSourceJobAgainstRunPolicy(authority, source); err != nil {
		return err
	}
	expected, err := completeSourceJobRecord(source)
	if err != nil {
		return err
	}
	for i, field := range expected {
		if field.Name != record[i].Name || !bytes.Equal(field.Value, record[i].Value) {
			return ErrRecordRelation
		}
	}
	return nil
}

func jobLuaCompareSource(t *testing.T, vm *jobLuaVM, ledger *lua.LTable, authority RunPolicyAuthority, record Record) {
	t.Helper()
	want := jobLuaSourceOracle(authority, record)
	result, code := vm.invoke(t, "Job", "source", jobLuaEncoded(t, record), ledger)
	if (result != lua.LNil) != (want == nil) {
		t.Fatalf("source Lua=%v/%v Go=%v", result, code, want)
	}
	if want == nil {
		if code != lua.LNil {
			t.Fatal(code)
		}
		jobLuaAssertRecord(t, result, record)
	} else if _, err := ParseErrorCode(code.String()); err != nil {
		t.Fatal("source rejection is not closed", code)
	}
}

func jobLuaApplySource(job, source Record) {
	for _, field := range source {
		for i := range job {
			if job[i].Name == field.Name || field.Name == "job_id" && job[i].Name == "url_id" {
				job[i].Value = append([]byte(nil), field.Value...)
			}
		}
	}
}

func TestJobLuaSourceDifferentialEveryField(t *testing.T) {
	t.Parallel()
	vm := jobLuaNew(t)
	source := jobLuaSourceValue(t, "https://example.com/path")
	groups := []PolicyGroup{jobLuaGroup(source)}
	ledger, _ := jobLuaBinding(t, vm, groups)
	authority := newAuthenticatedTestRunPolicyAuthority(t, RunID(strings.Repeat("1", 32)),
		Digest(strings.Repeat("a", 64)), Digest(strings.Repeat("b", 64)), groups)
	baseline, err := completeSourceJobRecord(source)
	if err != nil {
		t.Fatal(err)
	}
	jobLuaCompareSource(t, vm, ledger, authority, baseline)
	for i, field := range baseline {
		for _, value := range []string{"", "0", "1", "00", "-0", "9007199254740991", "9007199254740992", "\xff", "\x00",
			strings.Repeat("0", 32), strings.Repeat("0", 64), strings.Repeat("f", 64), strings.Repeat("é", 65)} {
			t.Run(field.Name+"/"+fmt.Sprintf("%x", value), func(t *testing.T) {
				candidate := cloneRecord(baseline)
				recordAuthoritySet(candidate, i, value)
				jobLuaCompareSource(t, vm, ledger, authority, candidate)
			})
		}
		missing := append(cloneRecord(baseline[:i]), cloneRecord(baseline[i+1:])...)
		jobLuaCompareSource(t, vm, ledger, authority, missing)
		wrongName := cloneRecord(baseline)
		wrongName[i].Name += "_wrong"
		jobLuaCompareSource(t, vm, ledger, authority, wrongName)
	}
	jobLuaCompareSource(t, vm, ledger, authority, append(cloneRecord(baseline), textField("url_id", string(source.JobID))))
	for _, url := range []string{"http://127.0.0.1/", "https://[2001:db8::1]/", "https://EXAMPLE.com/path", "https://bücher.example/",
		"https://xn--a.example/", "https://example.com:443/", "https://example.com/" + strings.Repeat("x", 2048)} {
		candidate := cloneRecord(baseline)
		recordAuthoritySet(candidate, 0, utils.URLIDV1(url))
		recordAuthoritySet(candidate, 1, url)
		jobLuaCompareSource(t, vm, ledger, authority, candidate)
	}
	// Wire.record projections are accepted, but only their ordered fields carry
	// input. Neither forged numeric/values maps nor redundant group indexes are
	// authority. Run.validate reparses the complete run values on each call.
	names := make([]string, len(baseline))
	for i, field := range baseline {
		names[i] = field.Name
	}
	projection, code := vm.invoke(t, "Wire", "record", jobLuaEncoded(t, baseline), bootLuaStrings(vm.state, names), lua.LNumber(16384))
	if code != lua.LNil {
		t.Fatal(code)
	}
	projection.(*lua.LTable).RawSetString("v", lua.LFalse)
	projection.(*lua.LTable).RawSetString("n", lua.LNumber(999))
	ledger.RawSetString("n", lua.LFalse)
	for _, name := range []string{"by_id", "count", "digest"} {
		ledger.RawGetString("groups").(*lua.LTable).RawSetString(name, lua.LFalse)
	}
	result, code := vm.invoke(t, "Job", "source", projection, ledger)
	if code != lua.LNil {
		t.Fatal(code)
	}
	jobLuaAssertRecord(t, result, baseline)
	projection.(*lua.LTable).RawGetString("fields").(*lua.LTable).RawGetInt(9).(*lua.LTable).RawSetInt(2, lua.LString(strings.Repeat("f", 64)))
	if _, code := vm.invoke(t, "Job", "source", projection, ledger); code != lua.LString("IMMUTABLE_MISMATCH") {
		t.Fatal("mutated fields bypassed validation", code)
	}
}

func TestJobLuaSourcesPolicyOrderingAndDiscoveryAdapter(t *testing.T) {
	t.Parallel()
	vm := jobLuaNew(t)
	source := jobLuaSourceValue(t, "https://example.com/path")
	group := jobLuaGroup(source)
	ledger, _ := jobLuaBinding(t, vm, []PolicyGroup{group})
	authority := newAuthenticatedTestRunPolicyAuthority(t, RunID(strings.Repeat("1", 32)),
		Digest(strings.Repeat("a", 64)), Digest(strings.Repeat("b", 64)), []PolicyGroup{group})
	for _, url := range []string{source.CanonicalURL, "https://xn--bcher-kva.example/a%2Fb", "https://example.com:8443/?q=%FF"} {
		candidate := jobLuaSourceValue(t, url)
		if err := validateSourceJobAgainstRunPolicy(authority, candidate); err != nil {
			t.Fatal(err)
		}
		r, err := completeSourceJobRecord(candidate)
		if err != nil {
			t.Fatal(err)
		}
		result, code := vm.invoke(t, "Job", "source", jobLuaEncoded(t, r), ledger)
		if code != lua.LNil {
			t.Fatal(code)
		}
		jobLuaAssertRecord(t, result, r)
		discovery, err := completeDiscoveryRecord(OutputDiscovery{JobID: candidate.JobID, CanonicalURL: candidate.CanonicalURL,
			Depth: candidate.Depth, ScoreText: candidate.ScoreText, GroupID: candidate.GroupID, RateScopeID: candidate.RateScopeID, Decision: candidate.Decision})
		if err != nil {
			t.Fatal(err)
		}
		adapted, code := vm.invoke(t, "Job", "discovery", jobLuaEncoded(t, discovery), ledger)
		if code != lua.LNil {
			t.Fatal(code)
		}
		jobLuaAssertRecord(t, adapted, r)
		if _, code := vm.invoke(t, "Job", "source", jobLuaEncoded(t, discovery), ledger); code != lua.LString("INVALID_ARGUMENT") {
			t.Fatalf("discovery accidentally treated as ENQUEUE: %v", code)
		}
	}
	r, _ := completeSourceJobRecord(source)
	for index, value := range map[int]string{0: strings.Repeat("b", 64), 1: "https://EXAMPLE.com/path", 2: "-0", 3: "01",
		4: "unknown", 5: strings.Repeat("3", 32), 6: strings.Repeat("a", 64), 7: strings.Repeat("a", 64), 8: strings.Repeat("b", 64)} {
		candidate := cloneRecord(r)
		recordAuthoritySet(candidate, index, value)
		if result, code := vm.invoke(t, "Job", "source", jobLuaEncoded(t, candidate), ledger); result != lua.LNil || code == lua.LNil {
			t.Fatalf("mutated source field %d accepted", index)
		}
	}
	for _, mutation := range []struct {
		index int
		value string
		code  string
	}{
		{0, strings.Repeat("b", 64), "URL_ID_COLLISION"}, {0, strings.Repeat("b", 32), "INVALID_IDENTIFIER"},
		{3, "9007199254740992", "INVALID_NUMBER"}, {3, "1", "IMMUTABLE_MISMATCH"},
		{8, strings.Repeat("c", 64), "IMMUTABLE_MISMATCH"},
	} {
		candidate := cloneRecord(r)
		recordAuthoritySet(candidate, mutation.index, mutation.value)
		if _, code := vm.invoke(t, "Job", "source", jobLuaEncoded(t, candidate), ledger); code != lua.LString(mutation.code) {
			t.Fatalf("source error context %d: %v, want %s", mutation.index, code, mutation.code)
		}
	}
	for _, url := range []string{"http://127.0.0.1/", "https://[2001:db8::1]/"} {
		candidate := cloneRecord(r)
		recordAuthoritySet(candidate, 0, utils.URLIDV1(url))
		recordAuthoritySet(candidate, 1, url)
		if _, code := vm.invoke(t, "Job", "source", jobLuaEncoded(t, candidate), ledger); code != lua.LString("INVALID_IDENTIFIER") {
			t.Fatalf("canonical IP identity granted source admission: %v", code)
		}
	}
	for _, mutate := range []func(*SourceJob){
		func(s *SourceJob) { s.Depth++; s.Decision.Depth++ },
		func(s *SourceJob) { s.Decision.GroupConcurrency++; s.Decision.OriginConcurrency++ },
		func(s *SourceJob) { s.Decision.GroupIntervalMS++; s.Decision.OriginIntervalMS++ },
	} {
		candidate := source
		mutate(&candidate)
		candidateRecord, err := completeSourceJobRecord(candidate)
		if err != nil {
			t.Fatal(err)
		}
		want := validateSourceJobAgainstRunPolicy(authority, candidate) == nil
		got, _ := vm.invoke(t, "Job", "source", jobLuaEncoded(t, candidateRecord), ledger)
		if (got != lua.LNil) != want {
			t.Fatal("Go/Lua group binding mismatch")
		}
	}
	// A valid map for a different policy is not the run's pinned map.
	wrong := group
	wrong.IntervalMS++
	other, _ := jobLuaBinding(t, vm, []PolicyGroup{wrong})
	other.RawSetString("v", ledger.RawGetString("v"))
	if _, code := vm.invoke(t, "Job", "source", jobLuaEncoded(t, r), other); code != lua.LString("IMMUTABLE_MISMATCH") {
		t.Fatalf("map digest mismatch: %v", code)
	}
	other.RawSetString("groups", lua.LNil)
	if _, code := vm.invoke(t, "Job", "source", jobLuaEncoded(t, r), other); code != lua.LString("INVALID_STATE") {
		t.Fatalf("unknown map became empty: %v", code)
	}
	second, _ := completeSourceJobRecord(jobLuaSourceValue(t, "https://example.com/second"))
	records := []Record{r, second}
	sort.Slice(records, func(i, j int) bool { return bytes.Compare(records[i][0].Value, records[j][0].Value) < 0 })
	array := vm.state.NewTable()
	for i, record := range records {
		array.RawSetInt(i+1, jobLuaEncoded(t, record))
	}
	if _, code := vm.invoke(t, "Job", "sources", array, ledger); code != lua.LNil {
		t.Fatal(code)
	}
	array.RawSetInt(1, jobLuaEncoded(t, records[1]))
	if _, code := vm.invoke(t, "Job", "sources", array, ledger); code != lua.LString("INVALID_ARGUMENT") {
		t.Fatal("duplicate sources accepted")
	}
	array.RawSetInt(2, jobLuaEncoded(t, records[0]))
	if _, code := vm.invoke(t, "Job", "sources", array, ledger); code != lua.LString("INVALID_ARGUMENT") {
		t.Fatal("descending sources sorted instead of rejected")
	}
	for _, count := range []int{0, 501} {
		array := vm.state.NewTable()
		for i := 1; i <= count; i++ {
			array.RawSetInt(i, jobLuaEncoded(t, r))
		}
		if result, code := vm.invoke(t, "Job", "sources", array, ledger); result != lua.LNil || code == lua.LNil {
			t.Fatalf("source count %d accepted", count)
		}
	}
	sparse := vm.state.NewTable()
	sparse.RawSetInt(2, jobLuaEncoded(t, r))
	if _, code := vm.invoke(t, "Job", "sources", sparse, ledger); code != lua.LString("INVALID_ARGUMENT") {
		t.Fatal("sparse source list accepted", code)
	}
	for _, input := range []lua.LValue{lua.LNil, lua.LFalse, lua.LNumber(0), vm.state.NewTable(), lua.LString("")} {
		if _, code := vm.invoke(t, "Job", "source", input, ledger); code != lua.LString("INVALID_ARGUMENT") {
			t.Fatal("malformed source container accepted", code)
		}
	}
}

// Minimal read-only in-memory RESP boundary; all validation and private receipt
// creation remain the real Context/Read modules. No synthetic receipt API.
type jobLuaRedis struct {
	hashes map[string]map[string]string
	sets   map[string]map[string]bool
	zsets  map[string]map[string]string
	trace  []string
	nowMS  uint64
}

func jobLuaStore() *jobLuaRedis {
	return &jobLuaRedis{hashes: map[string]map[string]string{}, sets: map[string]map[string]bool{}, zsets: map[string]map[string]string{}, nowMS: 1000000}
}

func (r *jobLuaRedis) hash(key string, record Record) {
	r.hashes[key] = map[string]string{}
	for _, f := range record {
		r.hashes[key][f.Name] = string(f.Value)
	}
}

func (r *jobLuaRedis) call(l *lua.LState) int {
	command := l.CheckString(1)
	r.trace = append(r.trace, command)
	if command == "TIME" {
		l.Push(bootLuaStrings(l, []string{canonicalDecimal(r.nowMS / 1000), canonicalDecimal((r.nowMS % 1000) * 1000)}))
		return 1
	}
	key := l.CheckString(2)
	switch command {
	case "TYPE":
		kind := "none"
		if _, ok := r.hashes[key]; ok {
			kind = "hash"
		}
		if _, ok := r.sets[key]; ok {
			kind = "set"
		}
		if _, ok := r.zsets[key]; ok {
			kind = "zset"
		}
		v := l.NewTable()
		v.RawSetString("ok", lua.LString(kind))
		l.Push(v)
	case "HLEN":
		l.Push(lua.LNumber(len(r.hashes[key])))
	case "HKEYS":
		var fields []string
		for field := range r.hashes[key] {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		l.Push(bootLuaStrings(l, fields))
	case "HSTRLEN":
		l.Push(lua.LNumber(len(r.hashes[key][l.CheckString(3)])))
	case "HMGET":
		v := l.NewTable()
		for i := 3; i <= l.GetTop(); i++ {
			value, found := r.hashes[key][l.CheckString(i)]
			if found {
				v.RawSetInt(i-2, lua.LString(value))
			} else {
				v.RawSetInt(i-2, lua.LFalse)
			}
		}
		l.Push(v)
	case "SCARD":
		l.Push(lua.LNumber(len(r.sets[key])))
	case "ZCARD":
		l.Push(lua.LNumber(len(r.zsets[key])))
	case "SMEMBERS":
		var members []string
		for member := range r.sets[key] {
			members = append(members, member)
		}
		sort.Strings(members)
		l.Push(bootLuaStrings(l, members))
	case "ZRANGE":
		var members, values []string
		for member := range r.zsets[key] {
			members = append(members, member)
		}
		sort.Slice(members, func(i, j int) bool {
			a, _ := strconv.ParseFloat(r.zsets[key][members[i]], 64)
			b, _ := strconv.ParseFloat(r.zsets[key][members[j]], 64)
			return a < b || a == b && members[i] < members[j]
		})
		start, err := strconv.Atoi(l.CheckString(3))
		if err != nil || start < 0 {
			l.RaiseError("fixture only supports nonnegative ZRANGE offsets")
			return 0
		}
		end, err := strconv.Atoi(l.CheckString(4))
		if err != nil || end < start || l.CheckString(5) != "WITHSCORES" {
			l.RaiseError("unexpected ZRANGE shape")
			return 0
		}
		for i := start; i <= end && i < len(members); i++ {
			values = append(values, members[i], r.zsets[key][members[i]])
		}
		l.Push(bootLuaStrings(l, values))
	case "SISMEMBER":
		if r.sets[key][l.CheckString(3)] {
			l.Push(lua.LNumber(1))
		} else {
			l.Push(lua.LNumber(0))
		}
	case "ZSCORE":
		value, found := r.zsets[key][l.CheckString(3)]
		if found {
			l.Push(lua.LString(value))
		} else {
			l.Push(lua.LFalse)
		}
	default:
		l.RaiseError("unplanned command %s", command)
		return 0
	}
	return 1
}

func jobLuaOpen(t *testing.T, vm *jobLuaVM, r *jobLuaRedis, keys []string) *lua.LTable {
	t.Helper()
	redis := vm.state.NewTable()
	redis.RawSetString("call", vm.state.NewFunction(r.call))
	vm.env.RawSetString("redis", redis)
	spec := vm.state.NewTable()
	spec.RawSetString("operation", lua.LString("CJ2_INSTALL_CANDIDATE_MARKERS"))
	spec.RawSetString("fields", bootLuaStrings(vm.state, []string{"run_id"}))
	names := make([]string, len(keys))
	for i := range names {
		names[i] = fmt.Sprintf("key_%d", i)
		if keys[i] == "mifolyo:crawl:v2:run:"+strings.Repeat("1", 32) {
			names[i] = "run"
		}
	}
	spec.RawSetString("key_names", bootLuaStrings(vm.state, names))
	spec.RawSetString("key_values", bootLuaStrings(vm.state, keys))
	spec.RawSetString("tail", lua.LString("none"))
	spec.RawSetString("request_limit", lua.LNumber(2097152))
	ctx, code := vm.invoke(t, "Context", "open", spec, bootLuaStrings(vm.state, keys),
		bootLuaStrings(vm.state, []string{"boot_only", strings.Repeat("1", 32), "", "", "", "", "", strings.Repeat("1", 32)}))
	if code != lua.LNil {
		t.Fatal(code)
	}
	return ctx.(*lua.LTable)
}

func TestJobLuaInitialRecordFullSentinelsAndClock(t *testing.T) {
	t.Parallel()
	vm := jobLuaNew(t)
	source := jobLuaSourceValue(t, "https://example.com/path")
	ledger, _ := jobLuaBinding(t, vm, []PolicyGroup{jobLuaGroup(source)})
	sourceRecord, _ := completeSourceJobRecord(source)
	r := jobLuaStore()
	ctx := jobLuaOpen(t, vm, r, []string{"mifolyo:crawl:v2:run:" + strings.Repeat("1", 32)})
	result, code := vm.invoke(t, "Job", "initial_record", ctx, ledger, jobLuaEncoded(t, sourceRecord))
	if code != lua.LNil {
		t.Fatal(code)
	}
	want := recordAuthorityJobRecord(t, "ready")
	recordAuthoritySet(want, jobPolicyDecisionSHA256Index, string(sourceRecord[8].Value))
	recordAuthoritySet(want, jobCreatedAtMSIndex, "1000000")
	recordAuthoritySet(want, jobUpdatedAtMSIndex, "1000000")
	if err := ValidateRecord(SchemaJob, want); err != nil {
		t.Fatal(err)
	}
	jobLuaAssertRecord(t, result, want)
	if len(r.trace) != 1 || r.trace[0] != "TIME" {
		t.Fatal("pure constructor read/wrote Redis")
	}
	if _, code := vm.invoke(t, "Job", "initial_record", vm.state.NewTable(), ledger, jobLuaEncoded(t, sourceRecord)); code != lua.LString("INVALID_STATE") {
		t.Fatal("caller clock/context accepted")
	}
	if _, code := vm.invoke(t, "Context", "seal", ctx); code != lua.LNil {
		t.Fatal(code)
	}
	if _, code := vm.invoke(t, "Job", "initial_record", ctx, ledger, jobLuaEncoded(t, sourceRecord)); code != lua.LString("INVALID_STATE") {
		t.Fatal("sealed context accepted")
	}
}

func TestJobLuaInitialMaximaAndContextBinding(t *testing.T) {
	t.Parallel()
	vm := jobLuaNew(t)
	url := "https://example.com/" + strings.Repeat("x", 2048-len("https://example.com/"))
	source := jobLuaSourceValue(t, url)
	source.Depth, source.Decision.Depth = MaxExactInteger, MaxExactInteger
	source.GroupID = GroupID(strings.Repeat("é", 64))
	source.Decision.GroupID, source.ScoreText = source.GroupID, "-999.999999"
	// A maximum 64-group map, not a substituted .by_id or .digest. Byte ordering
	// places this 128-byte UTF-8 group after the ASCII groups.
	groups := make([]PolicyGroup, 64)
	for i := range groups {
		groups[i] = jobLuaGroup(source)
		if i != len(groups)-1 {
			groups[i].GroupID = GroupID(fmt.Sprintf("group-%02d", i))
		}
	}
	ledger, _ := jobLuaBinding(t, vm, groups)
	authority := newAuthenticatedTestRunPolicyAuthority(t, RunID(strings.Repeat("1", 32)),
		Digest(strings.Repeat("a", 64)), Digest(strings.Repeat("b", 64)), groups)
	sourceRecord, err := completeSourceJobRecord(source)
	if err != nil {
		t.Fatal(err)
	}
	jobLuaCompareSource(t, vm, ledger, authority, sourceRecord)
	r := jobLuaStore()
	r.nowMS = MaxExactInteger
	ctx := jobLuaOpen(t, vm, r, []string{"mifolyo:crawl:v2:run:" + strings.Repeat("1", 32)})
	admitted, code := vm.invoke(t, "Job", "source", jobLuaEncoded(t, sourceRecord), ledger)
	if code != lua.LNil {
		t.Fatal(code)
	}
	result, code := vm.invoke(t, "Job", "initial_record", ctx, ledger, admitted)
	if code != lua.LNil {
		t.Fatal(code)
	}
	want := recordAuthorityJobRecord(t, "ready")
	jobLuaApplySource(want, sourceRecord)
	for _, index := range []int{jobCreatedAtMSIndex, jobUpdatedAtMSIndex} {
		recordAuthoritySet(want, index, canonicalDecimal(MaxExactInteger))
	}
	if err := ValidateRecord(SchemaJob, want); err != nil {
		t.Fatal(err)
	}
	jobLuaAssertRecord(t, result, want)
	if !jobLuaCompare(t, vm, want) {
		t.Fatal("maximum pure record rejected")
	}
	if len(r.trace) != 1 || r.trace[0] != "TIME" {
		t.Fatal("source/constructor performed I/O")
	}
	// Mutating a returned source cannot authorize the constructor through its
	// earlier successful validation, nor change the already-built full record.
	admitted.(*lua.LTable).RawGetString("fields").(*lua.LTable).RawGetInt(4).(*lua.LTable).RawSetInt(2, lua.LString("0"))
	if _, code := vm.invoke(t, "Job", "initial_record", ctx, ledger, admitted); code != lua.LString("IMMUTABLE_MISMATCH") {
		t.Fatal("constructor trusted stale source admission", code)
	}
	jobLuaAssertRecord(t, result, want)
	for _, test := range []struct {
		name   string
		mutate func(*lua.LTable)
	}{
		{"noncanonical-clock", func(ctx *lua.LTable) { ctx.RawSetString("now_text", lua.LString("01000000")) }},
		{"clock-mismatch", func(ctx *lua.LTable) { ctx.RawSetString("now_ms", lua.LNumber(2)) }},
		{"zero-clock", func(ctx *lua.LTable) {
			ctx.RawSetString("now_ms", lua.LNumber(0))
			ctx.RawSetString("now_text", lua.LString("0"))
		}},
		{"missing-run-key", func(ctx *lua.LTable) { ctx.RawGetString("keys").(*lua.LTable).RawSetString("run", lua.LNil) }},
		{"wrong-run-key", func(ctx *lua.LTable) {
			ctx.RawGetString("keys").(*lua.LTable).RawSetString("run", lua.LString("mifolyo:crawl:v2:run:"+strings.Repeat("2", 32)))
		}},
		{"wrong-request-run", func(ctx *lua.LTable) {
			ctx.RawGetString("request").(*lua.LTable).RawGetString("v").(*lua.LTable).RawSetString("run_id", lua.LString(strings.Repeat("2", 32)))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := jobLuaOpen(t, vm, r, []string{"mifolyo:crawl:v2:run:" + strings.Repeat("1", 32)})
			test.mutate(ctx)
			before := len(r.trace)
			if _, code := vm.invoke(t, "Job", "initial_record", ctx, ledger, jobLuaEncoded(t, sourceRecord)); code != lua.LString("INVALID_STATE") {
				t.Fatal("invalid context accepted", code)
			}
			if before != len(r.trace) {
				t.Fatal("rejection performed I/O")
			}
		})
	}
	r.nowMS = 99 // a real opened context preceding the run's created_at_ms=100
	ctx = jobLuaOpen(t, vm, r, []string{"mifolyo:crawl:v2:run:" + strings.Repeat("1", 32)})
	if _, code := vm.invoke(t, "Job", "initial_record", ctx, ledger, jobLuaEncoded(t, sourceRecord)); code != lua.LString("INVALID_STATE") {
		t.Fatal("constructor predates its run", code)
	}
}

func TestJobLuaActualRunLoadAndWireSource(t *testing.T) {
	t.Parallel()
	for _, operation := range []OperationName{OperationEnqueueBatch, OperationAuditRunBatch} {
		for _, leases := range []string{"absent", "other", "owned"} {
			t.Run(string(operation)+"/"+leases, func(t *testing.T) {
				jobLuaActualRunLoadAndWireSource(t, operation, leases)
			})
		}
	}
}

func jobLuaActualRunLoadAndWireSource(t *testing.T, operation OperationName, leases string) {
	t.Helper()
	vm := jobLuaNew(t)
	r := jobLuaStore()
	source := jobLuaSourceValue(t, "https://xn--bcher-kva.example/path?q=%FF")
	source.ScoreText = "0.1"
	group := jobLuaGroup(source)
	groups := []PolicyGroup{group}
	runID := strings.Repeat("1", 32)
	base := "mifolyo:crawl:v2:run:" + runID
	globalKey, globalMember := "mifolyo:crawl:v2:active_leases", runID+":"+string(source.JobID)
	a := newGateArtifacts(t)
	run := recordAuthorityRunRecord(t, "loading")
	digest, err := DerivePolicyGroupMapDigest(groups)
	if err != nil {
		t.Fatal(err)
	}
	for index, value := range map[int]string{runContractSHA256Index: string(a.contract), runPolicyGroupMapSHA256Index: string(digest),
		runJobCountIndex: "0", runOpenJobCountIndex: "0", runLoadRevisionIndex: "1", runAuthorizationExpiresAtMSIndex: "2000000"} {
		recordAuthoritySet(run, index, value)
	}
	wantSource, err := completeSourceJobRecord(source)
	if err != nil {
		t.Fatal(err)
	}
	stored := recordAuthorityJobRecord(t, "ready")
	jobLuaApplySource(stored, wantSource)
	if err := ValidateRecord(SchemaJob, stored); err != nil {
		t.Fatal(err)
	}
	// ENQUEUE checks an absent job; AUDIT checks an existing, source-bound ready
	// job. Both use complete Go-valid run ledgers, not a replaced Run.load.
	if operation == OperationAuditRunBatch {
		for index, value := range map[int]string{runStateIndex: "auditing", runJobCountIndex: "1", runOpenJobCountIndex: "1", runAuditRevisionIndex: "1"} {
			recordAuthoritySet(run, index, value)
		}
		r.hash(base+":job:"+string(source.JobID), stored)
		r.sets[base+":jobs"] = map[string]bool{string(source.JobID): true}
		r.zsets[base+":job_order"] = map[string]string{string(source.JobID): "0"}
		r.zsets[base+":ready"] = map[string]string{string(source.JobID): "1e-1"}
		r.zsets[base+":ready_at"] = map[string]string{string(source.JobID): string(stored[jobCreatedAtMSIndex].Value)}
		r.hashes[base+":audit_group_counts"] = map[string]string{string(group.GroupID): "0"}
	}
	authority, err := newTestTransportAuthority().parseRunPolicyAuthority(RunID(runID), run, groups)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := NewTransportGate(operation, activeGateInput(a))
	if err != nil {
		t.Fatal(err)
	}
	var request OperationWireRequest
	fields := []string{"run_id", "record_count"}
	if operation == OperationEnqueueBatch {
		request, err = NewEnqueueBatchWireRequest(gate, authority, RunID(runID), []SourceJob{source})
	} else {
		request, err = NewAuditRunBatchWireRequest(gate, authority, AuditRunBatchWireInput{RunID: RunID(runID), Jobs: []SourceJob{source}})
		fields = []string{"run_id", "expected_prior_cursor", "expected_prior_count", "record_count"}
	}
	if err != nil {
		t.Fatal(err)
	}
	keys, args, _, err := request.validatedWireParts()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 38+1 {
		t.Fatalf("one-source wire must remain 38+n, got %d keys", len(keys))
	}
	keyStrings, argStrings := make([]string, len(keys)), make([]string, len(args))
	for i := range keys {
		keyStrings[i] = string(keys[i])
		if keyStrings[i] == globalKey {
			t.Fatal("read-only global lease exception added a wire key")
		}
	}
	for i := range args {
		argStrings[i] = string(args[i])
	}
	r.hash(base, run)
	for name, value := range map[string]string{
		"group_limits": canonicalDecimal(group.RequestStartLimit), "group_rate_scope_ids": string(group.RateScopeID),
		"group_scope_ids": string(group.GroupScopeID), "group_concurrency": canonicalDecimal(group.Concurrency),
		"group_interval_ms": canonicalDecimal(group.IntervalMS), "group_started": "0", "group_pending": "0",
		"group_active_started": "0", "group_open_jobs": string(run[runOpenJobCountIndex].Value),
	} {
		r.hashes[base+":"+name] = map[string]string{string(group.GroupID): value}
	}
	r.hashes[base+":retry_reason_counts"] = map[string]string{}
	r.hashes[base+":disposition_reason_counts"] = map[string]string{}
	for reason := range reasons {
		if isRetryableReason(reason) {
			r.hashes[base+":retry_reason_counts"][string(reason)] = "0"
		}
		if isDeadLetterReason(reason) || isCancellationReason(reason) || reason == ReasonPublished || reason == ReasonAlreadyVisited {
			r.hashes[base+":disposition_reason_counts"][string(reason)] = "0"
		}
	}
	r.hashes[base+":recovery_outcome_counts"] = map[string]string{"ready": "0", "delayed": "0", "dead": "0", "cancelled": "0"}
	r.zsets["mifolyo:crawl:v2:runs"] = map[string]string{runID: string(run[runCreatedAtMSIndex].Value)}
	r.sets["mifolyo:crawl:v2:active_runs"] = map[string]bool{runID: true}
	r.sets["mifolyo:crawl:v2:unarchived_runs"] = map[string]bool{runID: true}
	switch leases {
	case "other":
		r.zsets[globalKey] = map[string]string{strings.Repeat("2", 32) + ":" + strings.Repeat("e", 64): "1e3"}
	case "owned":
		// An absent/ready job cannot own a lease. The actual global receipt must
		// expose this corruption, not synthesize false because the run is pre-I/O.
		r.zsets[globalKey] = map[string]string{globalMember: "1e3"}
	}
	redis := vm.state.NewTable()
	redis.RawSetString("call", vm.state.NewFunction(r.call))
	vm.env.RawSetString("redis", redis)
	spec, code := vm.invoke(t, "Wire", "run_spec", lua.LString(operation), bootLuaStrings(vm.state, fields))
	if code != lua.LNil {
		t.Fatal(code)
	}
	ctx, code := vm.invoke(t, "Context", "open", spec, bootLuaStrings(vm.state, keyStrings), bootLuaStrings(vm.state, argStrings))
	if code != lua.LNil {
		t.Fatal(code)
	}
	context := ctx.(*lua.LTable)
	if context.RawGetString("keys").(*lua.LTable).RawGetString("active_leases") != lua.LString(globalKey) ||
		context.RawGetString("allowed").(*lua.LTable).RawGetString(globalKey) != lua.LNil {
		t.Fatal("global lease convenience must be read-only, outside the wire-key set")
	}
	if allowed, code := vm.invoke(t, "Context", "can_read", ctx, lua.LString(globalKey)); allowed != lua.LTrue || code != lua.LNil {
		t.Fatal("operation did not receive the literal read grant", allowed, code)
	}
	ledger, code := vm.invoke(t, "Run", "load", ctx, lua.LString(runID))
	if code != lua.LNil {
		t.Fatal("actual Run.load failed", code)
	}
	before := len(r.trace)
	record := ctx.(*lua.LTable).RawGetString("request").(*lua.LTable).RawGetString("records").(*lua.LTable).RawGetInt(1)
	admitted, code := vm.invoke(t, "Job", "source", record, ledger)
	if code != lua.LNil {
		t.Fatal(code)
	}
	jobLuaAssertRecord(t, admitted, wantSource)
	initial, code := vm.invoke(t, "Job", "initial_record", ctx, ledger, admitted)
	if code != lua.LNil {
		t.Fatal(code)
	}
	want := recordAuthorityJobRecord(t, "ready")
	jobLuaApplySource(want, wantSource)
	recordAuthoritySet(want, jobCreatedAtMSIndex, canonicalDecimal(r.nowMS))
	recordAuthoritySet(want, jobUpdatedAtMSIndex, canonicalDecimal(r.nowMS))
	if err := ValidateRecord(SchemaJob, want); err != nil {
		t.Fatal(err)
	}
	jobLuaAssertRecord(t, initial, want)
	if len(r.trace) != before {
		t.Fatal("Job helpers fetched/wrote state after Run.load")
	}
	// Permission to read is not a receipt. Before the explicit global member
	// selection, even a complete-looking public absent fact cannot prove absence.
	view := vm.state.NewTable()
	view.RawSetString("ctx", ctx)
	fact, code := vm.invoke(t, "Read", "fixed_hash", ctx, lua.LString(base+":job:"+string(source.JobID)), lua.LString("job"))
	if code != lua.LNil {
		t.Fatal(code)
	}
	view.RawSetString("job", fact)
	for _, name := range jobLuaIndexNames {
		if name == "active_leases" {
			continue
		}
		key, member, kind, maximum := base+":"+name, string(source.JobID), "zset", 10000
		if name == "jobs" {
			kind = "set"
		} else if name == "leased" || name == "leased_at" {
			maximum = 64
		} else if name == "commit_backpressure" {
			maximum = 10
		}
		fact, code := vm.invoke(t, "Read", "members", ctx, lua.LString(key), lua.LString(kind), bootLuaStrings(vm.state, []string{member}), lua.LNumber(maximum), lua.LNumber(97))
		if code != lua.LNil {
			t.Fatal(code)
		}
		view.RawSetString(name, fact)
	}
	fake := vm.state.NewTable()
	fake.RawSetString("key", lua.LString(globalKey))
	fake.RawSetString("exists", lua.LFalse)
	fake.RawSetString("kind", lua.LString("none"))
	fake.RawSetString("count", lua.LNumber(0))
	fake.RawSetString("complete", lua.LTrue)
	for _, name := range []string{"members", "scores", "score_text"} {
		values := vm.state.NewTable()
		values.RawSetString(globalMember, lua.LFalse)
		fake.RawSetString(name, values)
	}
	view.RawSetString("active_leases", fake)
	before = len(r.trace)
	if _, code := vm.invoke(t, "Job", "check", view, ledger, lua.LString(source.JobID)); code != lua.LString("INVALID_STATE") {
		t.Fatal("missing global fact became absence", code)
	}
	if len(r.trace) != before {
		t.Fatal("Job.check fetched missing facts")
	}
	cardinality, code := vm.invoke(t, "Read", "cardinality", ctx, lua.LString(globalKey), lua.LString("zset"), lua.LNumber(64))
	if code != lua.LNil {
		t.Fatal("read-only cardinality failed", code)
	}
	cardinality.(*lua.LTable).RawGetString("members").(*lua.LTable).RawSetString(globalMember, lua.LFalse)
	view.RawSetString("active_leases", cardinality)
	before = len(r.trace)
	if _, code := vm.invoke(t, "Job", "check", view, ledger, lua.LString(source.JobID)); code != lua.LString("INVALID_STATE") {
		t.Fatal("unselected global member silently defaulted", code)
	}
	if len(r.trace) != before {
		t.Fatal("Job.check performed an implicit member read")
	}
	global, code := vm.invoke(t, "Read", "members", ctx, lua.LString(globalKey), lua.LString("zset"),
		bootLuaStrings(vm.state, []string{globalMember}), lua.LNumber(64), lua.LNumber(97))
	if code != lua.LNil {
		t.Fatal("explicit read-only member receipt failed", code)
	}
	g := global.(*lua.LTable)
	if g.RawGetString("members").(*lua.LTable).RawGetString(globalMember) != lua.LBool(leases == "owned") {
		t.Fatal("global membership did not reflect actual stored state")
	}
	if leases == "owned" && (g.RawGetString("scores").(*lua.LTable).RawGetString(globalMember) != lua.LNumber(1000) ||
		g.RawGetString("score_text").(*lua.LTable).RawGetString(globalMember) != lua.LString("1e3")) {
		t.Fatal("scientific score value/raw text changed")
	}
	view.RawSetString("active_leases", global)
	before = len(r.trace)
	checked, code := vm.invoke(t, "Job", "check", view, ledger, lua.LString(source.JobID))
	if leases == "owned" {
		if checked != lua.LNil || code != lua.LString("STATE_INDEX_CORRUPT") {
			t.Fatal("owned global lease on absent/ready job was hidden", code)
		}
	} else {
		if code != lua.LNil {
			t.Fatal("Job.check rejected explicit global absence", code)
		}
		if checked.(*lua.LTable).RawGetString("exists") != lua.LBool(operation == OperationAuditRunBatch) {
			t.Fatal("wrong checked job existence")
		}
		if operation == OperationAuditRunBatch {
			jobLuaAssertRecord(t, checked, stored)
		}
	}
	if len(r.trace) != before {
		t.Fatal("Job.check performed I/O after receiving all facts")
	}
	jobLuaAssertGlobalWritesDenied(t, vm, r, context, globalMember)
	before = len(r.trace)
	if _, code := vm.invoke(t, "Read", "members", ctx, lua.LString(globalKey+":unexpected"), lua.LString("zset"),
		bootLuaStrings(vm.state, []string{globalMember}), lua.LNumber(64), lua.LNumber(97)); code != lua.LString("INVALID_ARGUMENT") {
		t.Fatal("literal exception became a key-prefix grant", code)
	}
	if len(r.trace) != before {
		t.Fatal("unauthorized read reached the RESP boundary")
	}
}

func jobLuaAssertGlobalWritesDenied(t *testing.T, vm *jobLuaVM, r *jobLuaRedis, ctx *lua.LTable, member string) {
	t.Helper()
	const global = "mifolyo:crawl:v2:active_leases"
	base := ctx.RawGetString("keys").(*lua.LTable).RawGetString("run").String()
	allowed := ctx.RawGetString("allowed").(*lua.LTable)
	original := allowed.RawGetString(global)
	defer allowed.RawSetString(global, original)
	before := len(r.trace)
	plan, code := vm.invoke(t, "Plan", "new", ctx)
	if code != lua.LNil {
		t.Fatal(code)
	}
	for _, forged := range []bool{false, true} {
		if forged {
			allowed.RawSetString(global, lua.LTrue)
		}
		if writable, code := vm.invoke(t, "Context", "can_write", ctx, lua.LString(global)); writable != lua.LFalse || code != lua.LNil {
			t.Fatal("internal read/public allowed edit granted a write", writable, code)
		}
		for _, argv := range [][]string{{"ZADD", global, "1000", member}, {"ZREM", global, member}, {"UNLINK", global},
			{"RENAME", base, global}, {"RENAME", global, base}} {
			if result, code := vm.invoke(t, "Plan", "add", plan, bootLuaStrings(vm.state, argv), lua.LString("ordinary")); result != lua.LNil || code != lua.LString("INVALID_ARGUMENT") {
				t.Fatalf("%s write escaped read-only grant (forged=%t): %v/%v", argv[0], forged, result, code)
			}
		}
	}
	assessment, code := vm.invoke(t, "Plan", "assess", ctx, plan)
	if code != lua.LNil || assessment.(*lua.LTable).RawGetString("growth") != lua.LNumber(0) {
		t.Fatal("rejected writes left a nonempty plan", code)
	}
	// Positive control: this is a key-specific denial, not a broken/locked Plan.
	control, code := vm.invoke(t, "Plan", "new", ctx)
	if code != lua.LNil {
		t.Fatal(code)
	}
	if result, code := vm.invoke(t, "Plan", "add", control, bootLuaStrings(vm.state, []string{"HSET", base, "load_revision", "1"}),
		lua.LString("ordinary")); result != lua.LTrue || code != lua.LNil {
		t.Fatal("ordinary wire-key descriptor was denied", result, code)
	}
	if len(r.trace) != before {
		t.Fatal("permission/empty-plan checks performed I/O")
	}
}

func TestJobLuaReadOnlyLeaseReceiptOperationBoundary(t *testing.T) {
	t.Parallel()
	vm := jobLuaNew(t)
	r := jobLuaStore()
	runID := strings.Repeat("1", 32)
	const global = "mifolyo:crawl:v2:active_leases"
	member := runID + ":" + strings.Repeat("e", 64)
	a := newGateArtifacts(t)
	gate, err := NewTransportGate(OperationBeginRunAudit, activeGateInput(a))
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewBeginRunAuditWireRequest(gate, RunID(runID))
	if err != nil {
		t.Fatal(err)
	}
	keys, args, _, err := request.validatedWireParts()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 38 {
		t.Fatal("BEGIN_RUN_AUDIT wire key count changed")
	}
	keyStrings, argStrings := make([]string, len(keys)), make([]string, len(args))
	for i := range keys {
		keyStrings[i] = string(keys[i])
	}
	for i := range args {
		argStrings[i] = string(args[i])
	}
	redis := vm.state.NewTable()
	redis.RawSetString("call", vm.state.NewFunction(r.call))
	vm.env.RawSetString("redis", redis)
	spec, code := vm.invoke(t, "Wire", "run_spec", lua.LString(OperationBeginRunAudit), bootLuaStrings(vm.state, []string{"run_id"}))
	if code != lua.LNil {
		t.Fatal(code)
	}
	opened, code := vm.invoke(t, "Context", "open", spec, bootLuaStrings(vm.state, keyStrings), bootLuaStrings(vm.state, argStrings))
	if code != lua.LNil {
		t.Fatal(code)
	}
	ctx := opened.(*lua.LTable)
	if ctx.RawGetString("keys").(*lua.LTable).RawGetString("active_leases") != lua.LNil {
		t.Fatal("non-exempt operation received global lease convenience")
	}
	// A nearby run operation with the same AUTH+RUNS+RUN prefix cannot acquire
	// the exception by editing public operation, keys or allowed projections.
	for _, operation := range []OperationName{OperationBeginRunAudit, OperationEnqueueBatch, OperationAuditRunBatch} {
		t.Run(string(operation), func(t *testing.T) {
			if operation != OperationBeginRunAudit {
				ctx.RawSetString("operation", lua.LString(operation))
				ctx.RawGetString("keys").(*lua.LTable).RawSetString("active_leases", lua.LString(global))
				ctx.RawGetString("allowed").(*lua.LTable).RawSetString(global, lua.LTrue)
			}
			before := len(r.trace)
			if allowed, code := vm.invoke(t, "Context", "can_read", ctx, lua.LString(global)); allowed != lua.LFalse || code != lua.LNil {
				t.Fatal("non-exempt private operation received a read grant", allowed, code)
			}
			if _, code := vm.invoke(t, "Read", "members", ctx, lua.LString(global), lua.LString("zset"),
				bootLuaStrings(vm.state, []string{member}), lua.LNumber(64), lua.LNumber(97)); code != lua.LString("INVALID_ARGUMENT") {
				t.Fatal("non-exempt member read accepted", code)
			}
			if _, code := vm.invoke(t, "Context", "call", ctx, lua.LString("TYPE"), lua.LString(global)); code != lua.LString("INVALID_ARGUMENT") {
				t.Fatal("low-level read gateway bypassed operation restriction", code)
			}
			jobLuaAssertGlobalWritesDenied(t, vm, r, ctx, member)
			if len(r.trace) != before {
				t.Fatal("denied read/write reached the RESP boundary")
			}
		})
	}
}

var jobLuaIndexNames = []string{"jobs", "job_order", "ready", "ready_at", "leased", "leased_at", "delayed",
	"completed", "dead", "cancelled", "commit_backpressure", "active_leases"}

func jobLuaCheckView(t *testing.T, vm *jobLuaVM, record Record, absent bool, omit string,
	mutate func(*jobLuaRedis, string, string)) (*lua.LTable, *lua.LTable, *jobLuaRedis, lua.LValue) {
	t.Helper()
	source := jobLuaSourceValue(t, string(record[jobCanonicalURLIndex].Value))
	group := jobLuaGroup(source)
	ledger, run := jobLuaBinding(t, vm, []PolicyGroup{group})
	sourceRecord, _ := completeSourceJobRecord(source)
	record = cloneRecord(record)
	recordAuthoritySet(record, jobPolicyDecisionSHA256Index, string(sourceRecord[8].Value))
	r := jobLuaStore()
	base := "mifolyo:crawl:v2:run:" + strings.Repeat("1", 32)
	jobID := string(record[jobJobIDIndex].Value)
	jobKey := base + ":job:" + jobID
	r.hash(base, run)
	keys := []string{base, jobKey}
	policyMaps := map[string]string{"group_limits": "10", "group_rate_scope_ids": string(group.RateScopeID),
		"group_scope_ids": string(group.GroupScopeID), "group_concurrency": "3", "group_interval_ms": "100"}
	for name, value := range policyMaps {
		key := base + ":" + name
		keys = append(keys, key)
		r.hashes[key] = map[string]string{string(group.GroupID): value}
	}
	if !absent {
		r.hash(jobKey, record)
		r.sets[base+":jobs"] = map[string]bool{jobID: true}
		r.zsets[base+":job_order"] = map[string]string{jobID: "0"}
		primary := string(record[jobStateIndex].Value)
		index := map[string]int{"ready": jobScoreTextIndex, "leased": jobLeaseExpiresAtMSIndex, "delayed": jobNotBeforeMSIndex,
			"completed": jobCompletedAtMSIndex, "dead": jobDeadAtMSIndex, "cancelled": jobCancelledAtMSIndex}[primary]
		r.zsets[base+":"+primary] = map[string]string{jobID: string(record[index].Value)}
		if primary == "ready" {
			r.zsets[base+":ready_at"] = map[string]string{jobID: string(record[jobUpdatedAtMSIndex].Value)}
		}
		if primary == "leased" {
			r.zsets[base+":leased_at"] = map[string]string{jobID: string(record[jobLeaseStartedAtMSIndex].Value)}
			r.zsets["mifolyo:crawl:v2:active_leases"] = map[string]string{strings.Repeat("1", 32) + ":" + jobID: string(record[jobLeaseExpiresAtMSIndex].Value)}
		}
		if string(record[jobCommitBackpressureStartedAtMSIndex].Value) != "0" {
			r.zsets[base+":commit_backpressure"] = map[string]string{jobID: string(record[jobCommitBackpressureStartedAtMSIndex].Value)}
		}
	}
	for _, name := range jobLuaIndexNames {
		key := base + ":" + name
		if name == "active_leases" {
			key = "mifolyo:crawl:v2:active_leases"
		}
		keys = append(keys, key)
	}
	if mutate != nil {
		mutate(r, base, jobID)
	}
	sort.Strings(keys)
	ctx := jobLuaOpen(t, vm, r, keys)
	view := vm.state.NewTable()
	view.RawSetString("ctx", ctx)
	if _, code := vm.invoke(t, "Read", "fixed_hash", ctx, lua.LString(base), lua.LString("run")); code != lua.LNil {
		return view, ledger, r, code
	}
	for name := range policyMaps {
		if name == omit {
			continue
		}
		if _, code := vm.invoke(t, "Read", "dynamic_hash", ctx, lua.LString(base+":"+name), lua.LNumber(64), lua.LNumber(128), lua.LNumber(64)); code != lua.LNil {
			return view, ledger, r, code
		}
	}
	if omit != "job" {
		fact, code := vm.invoke(t, "Read", "fixed_hash", ctx, lua.LString(jobKey), lua.LString("job"))
		if code != lua.LNil {
			return view, ledger, r, code
		}
		view.RawSetString("job", fact)
	}
	for _, name := range jobLuaIndexNames {
		key, member, kind, maximum := base+":"+name, jobID, "zset", 10000
		if name == "jobs" {
			kind = "set"
		}
		if name == "leased" || name == "leased_at" {
			maximum = 64
		}
		if name == "commit_backpressure" {
			maximum = 10
		}
		if name == "active_leases" {
			key, member, maximum = "mifolyo:crawl:v2:active_leases", strings.Repeat("1", 32)+":"+jobID, 64
		}
		var fact, code lua.LValue
		if name == omit {
			// TYPE/cardinality and a forged public absent member cannot substitute
			// for a private selected-member receipt.
			if kind == "set" {
				r.sets[key] = map[string]bool{strings.Repeat("e", 64): true}
			} else {
				r.zsets[key] = map[string]string{strings.Repeat("e", 64): "100"}
			}
			fact, code = vm.invoke(t, "Read", "cardinality", ctx, lua.LString(key), lua.LString(kind), lua.LNumber(maximum))
			if code == lua.LNil {
				fact.(*lua.LTable).RawGetString("members").(*lua.LTable).RawSetString(member, lua.LFalse)
			}
		} else {
			fact, code = vm.invoke(t, "Read", "members", ctx, lua.LString(key), lua.LString(kind), bootLuaStrings(vm.state, []string{member}), lua.LNumber(maximum), lua.LNumber(97))
		}
		if code != lua.LNil {
			return view, ledger, r, code
		}
		view.RawSetString(name, fact)
	}
	return view, ledger, r, lua.LNil
}

func TestJobLuaMembershipReceiptsAllStatesAndNoReads(t *testing.T) {
	t.Parallel()
	vm := jobLuaNew(t)
	for name, record := range jobLuaFixtures(t) {
		t.Run(name, func(t *testing.T) {
			view, ledger, r, code := jobLuaCheckView(t, vm, record, false, "", nil)
			if code != lua.LNil {
				t.Fatal(code)
			}
			before := len(r.trace)
			result, code := vm.invoke(t, "Job", "check", view, ledger, lua.LString(record[jobJobIDIndex].Value))
			if code != lua.LNil {
				t.Fatal(code)
			}
			if len(r.trace) != before {
				t.Fatal("Job.check fetched a fact or wrote Redis")
			}
			p := result.(*lua.LTable)
			if p.RawGetString("schema") != lua.LString("job") || p.RawGetString("fields").(*lua.LTable).Len() != 54 {
				t.Fatal("partial job")
			}
			for _, f := range record {
				if _, err := ParseUnsignedDecimal(string(f.Value)); err == nil {
					n, _ := strconv.ParseUint(string(f.Value), 10, 64)
					if p.RawGetString("n").(*lua.LTable).RawGetString(f.Name) != lua.LNumber(n) {
						t.Fatalf("numeric projection omitted %s", f.Name)
					}
				}
			}
			// Stale lease vs invalid state: TIME is 1000000 but lease expiry is
			// 1000. Record/index validation succeeds; a worker handler decides stale.
			view.RawGetString("job").(*lua.LTable).RawGetString("v").(*lua.LTable).RawSetString("request_starts", lua.LString("999"))
			if _, code := vm.invoke(t, "Job", "check", view, ledger, lua.LString(record[jobJobIDIndex].Value)); code != lua.LNil {
				t.Fatal("public projection mutation changed private receipt")
			}
		})
	}
	ready := recordAuthorityJobRecord(t, "ready")
	view, ledger, _, code := jobLuaCheckView(t, vm, ready, true, "", nil)
	if code != lua.LNil {
		t.Fatal(code)
	}
	result, code := vm.invoke(t, "Job", "check", view, ledger, lua.LString(ready[jobJobIDIndex].Value))
	if code != lua.LNil || result.(*lua.LTable).RawGetString("exists") != lua.LFalse {
		t.Fatal("explicit absence rejected")
	}
	omitted := append([]string{"group_limits", "group_rate_scope_ids", "group_scope_ids", "group_concurrency", "group_interval_ms", "job"}, jobLuaIndexNames...)
	for _, omit := range omitted {
		view, ledger, _, code := jobLuaCheckView(t, vm, ready, false, omit, nil)
		if code != lua.LNil {
			t.Fatal(code)
		}
		if _, code := vm.invoke(t, "Job", "check", view, ledger, lua.LString(ready[jobJobIDIndex].Value)); code != lua.LString("INVALID_STATE") {
			t.Fatalf("unknown %s silently accepted: %v", omit, code)
		}
	}
}

func TestJobLuaMembershipCorruptionAndScoreRoundtrip(t *testing.T) {
	t.Parallel()
	vm := jobLuaNew(t)
	fixtures := jobLuaFixtures(t)
	for _, name := range jobLuaIndexNames {
		record := fixtures["staged"]
		if name == "ready" || name == "ready_at" {
			record = fixtures["ready"]
		}
		if name == "delayed" || name == "completed" || name == "dead" || name == "cancelled" {
			record = fixtures[name]
		}
		view, ledger, _, code := jobLuaCheckView(t, vm, record, false, "", func(r *jobLuaRedis, base, id string) {
			key := base + ":" + name
			if name == "active_leases" {
				key = "mifolyo:crawl:v2:active_leases"
			}
			delete(r.sets, key)
			delete(r.zsets, key)
		})
		if code != lua.LNil {
			t.Fatal(code)
		}
		if _, code := vm.invoke(t, "Job", "check", view, ledger, lua.LString(record[jobJobIDIndex].Value)); code != lua.LString("STATE_INDEX_CORRUPT") {
			t.Fatalf("missing %s: %v", name, code)
		}
	}
	for _, extra := range []string{"delayed", "leased_at", "commit_backpressure", "active_leases"} {
		record := fixtures["ready"]
		view, ledger, _, code := jobLuaCheckView(t, vm, record, false, "", func(r *jobLuaRedis, base, id string) {
			key := base + ":" + extra
			if extra == "active_leases" {
				key, id = "mifolyo:crawl:v2:active_leases", strings.Repeat("1", 32)+":"+id
			}
			r.zsets[key] = map[string]string{id: "100"}
		})
		if code != lua.LNil {
			t.Fatal(code)
		}
		if _, code := vm.invoke(t, "Job", "check", view, ledger, lua.LString(record[jobJobIDIndex].Value)); code != lua.LString("STATE_INDEX_CORRUPT") {
			t.Fatalf("extra %s accepted", extra)
		}
	}
	for _, raw := range []string{"0.10000000000000001", "1e-1", "0.10000000000000002", "NaN", "inf", "0", "-0"} {
		for _, canonical := range []string{"0.1", "0"} {
			t.Run("score/"+canonical+"/"+raw, func(t *testing.T) {
				record := cloneRecord(fixtures["ready"])
				recordAuthoritySet(record, jobScoreTextIndex, canonical)
				view, ledger, _, code := jobLuaCheckView(t, vm, record, false, "", func(r *jobLuaRedis, base, id string) {
					r.zsets[base+":ready"][id] = raw
				})
				var result lua.LValue = lua.LNil
				if code == lua.LNil {
					result, code = vm.invoke(t, "Job", "check", view, ledger, lua.LString(record[jobJobIDIndex].Value))
				}
				want := ValidateRedisScore(ScoreText(canonical), raw) == nil
				if (result != lua.LNil) != want {
					parsed, failure := vm.invoke(t, "Identities", "redis_score", lua.LString(raw))
					t.Fatalf("score %s/%s Lua parity: %v; common parser=%v/%v", canonical, raw, code, parsed, failure)
				}
			})
		}
	}
}

func TestJobLuaReceiptBoundsAndStoredPolicyMismatch(t *testing.T) {
	t.Parallel()
	vm := jobLuaNew(t)
	record := recordAuthorityJobRecord(t, "leased")
	for _, test := range []struct {
		name   string
		mutate func(*jobLuaRedis, string, string)
	}{
		{"wrong-type", func(r *jobLuaRedis, base, id string) {
			delete(r.sets, base+":jobs")
			r.hashes[base+":jobs"] = map[string]string{id: "0"}
		}},
		{"global-cap", func(r *jobLuaRedis, base, id string) {
			for i := 0; i < 64; i++ {
				r.zsets["mifolyo:crawl:v2:active_leases"][strings.Repeat("2", 32)+":"+fmt.Sprintf("%064x", i)] = "1000"
			}
		}},
		{"backpressure-cap", func(r *jobLuaRedis, base, id string) {
			r.zsets[base+":commit_backpressure"] = map[string]string{}
			for i := 0; i < 11; i++ {
				r.zsets[base+":commit_backpressure"][fmt.Sprintf("%064x", i)] = "100"
			}
		}},
		{"wrong-lease-score", func(r *jobLuaRedis, base, id string) { r.zsets[base+":leased"][id] = "1001" }},
		{"wrong-age-score", func(r *jobLuaRedis, base, id string) { r.zsets[base+":leased_at"][id] = "99" }},
		{"wrong-global-score", func(r *jobLuaRedis, base, id string) {
			r.zsets["mifolyo:crawl:v2:active_leases"][strings.Repeat("1", 32)+":"+id] = "1001"
		}},
		{"unknown-map-field", func(r *jobLuaRedis, base, id string) {
			delete(r.hashes[base+":group_limits"], "default")
			r.hashes[base+":group_limits"]["other"] = "10"
		}},
		{"changed-map-tuple", func(r *jobLuaRedis, base, id string) { r.hashes[base+":group_concurrency"]["default"] = "4" }},
		{"stored-policy-digest", func(r *jobLuaRedis, base, id string) {
			r.hashes[base+":job:"+id]["policy_decision_sha256"] = strings.Repeat("f", 64)
		}},
		{"stored-run-binding", func(r *jobLuaRedis, base, id string) {
			r.hashes[base+":job:"+id]["run_id"] = strings.Repeat("a", 32)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			view, ledger, _, code := jobLuaCheckView(t, vm, record, false, "", test.mutate)
			if code == lua.LNil {
				_, code = vm.invoke(t, "Job", "check", view, ledger, lua.LString(record[jobJobIDIndex].Value))
			}
			if code == lua.LNil {
				t.Fatal("corruption accepted")
			}
			if _, err := ParseErrorCode(code.String()); err != nil {
				t.Fatal(code)
			}
		})
	}
	view, ledger, _, code := jobLuaCheckView(t, vm, record, false, "", nil)
	if code != lua.LNil {
		t.Fatal(code)
	}
	ledger.RawGetString("v").(*lua.LTable).RawSetString("crawl_policy_sha256", lua.LString(strings.Repeat("c", 64)))
	if _, code := vm.invoke(t, "Job", "check", view, ledger, lua.LString(record[jobJobIDIndex].Value)); code != lua.LString("INVALID_STATE") {
		t.Fatal("run projection not authenticated against private receipt")
	}
}

func jobLuaQuote(s []byte) string {
	var out strings.Builder
	out.WriteByte('"')
	for _, b := range s {
		// Lua 5.1 has decimal escapes; never depend on Go's \x or Unicode escapes.
		fmt.Fprintf(&out, "\\%03d", b)
	}
	out.WriteByte('"')
	return out.String()
}

func TestJobLuaNativeFactoryParity(t *testing.T) {
	t.Parallel()
	if runtime.Version() != "go1.25.13" {
		t.Fatal("native Lua oracle requires Go 1.25.13")
	}
	executable, err := exec.LookPath("luajit")
	if err != nil {
		t.Skip("optional native Lua 5.1/LuaBitOp factory check needs luajit; no interpreter is installed by tests")
	}
	var program, want strings.Builder
	program.WriteString("local P=(function()\n" + string(primitiveLuaRead(t, "lua_src/primitives.lua")) + "\nend)()\nlocal CJ={P=P}\n")
	program.WriteString("local D=(function()\n" + string(primitiveLuaRead(t, "lua_src/unicode_data.lua")) + "\nend)()\n")
	program.WriteString("CJ.URL=(function()\n" + string(primitiveLuaRead(t, "lua_src/url.lua")) + "\nend)()\n")
	for _, module := range []struct{ name, file string }{{"Identities", "identities"}, {"Schemas", "schemas"}, {"Job", "ledger_job"}} {
		program.WriteString("CJ." + module.name + "=(function()\n" + string(primitiveLuaRead(t, "lua_src/"+module.file+".lua")) + "\nend)()\n")
	}
	program.WriteString("local records={\n")
	count := 0
	appendCase := func(record Record) {
		encoded, err := EncodeRecord(record)
		if err != nil {
			t.Fatal(err)
		}
		program.WriteString(jobLuaQuote(encoded) + ",\n")
		if ValidateRecord(SchemaJob, record) == nil {
			want.WriteString("1\n")
		} else {
			want.WriteString("0\n")
		}
		count++
	}
	for _, record := range jobLuaFixtures(t) {
		appendCase(record)
		for index := range record {
			candidate := cloneRecord(record)
			recordAuthoritySet(candidate, index, "invalid")
			appendCase(candidate)
		}
	}
	program.WriteString(`}
for i=1,#records do
    local p,code=CJ.Schemas.decode("job",records[i])
    if p then
        assert(code==nil and CJ.Schemas.encode(p)==records[i])
        io.write("1\n")
    else
        assert(type(code)=="string")
        io.write("0\n")
    end
end
local number,code=CJ.Identities.redis_score("1e-1")
assert(number==0.1 and code==nil)
local negative_zero=assert(CJ.Identities.redis_score("-0"))
assert(negative_zero==0 and 1/negative_zero==-math.huge)
io.write("native-numbers-ok\n")
`)
	want.WriteString("native-numbers-ok\n")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-")
	cmd.Stdin = strings.NewReader(program.String())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pure native factories: %v\n%s", err, output)
	}
	if string(output) != want.String() {
		t.Fatal("native Lua schema verdicts/roundtrips differ from pinned Go")
	}
	t.Logf("native Lua 5.1/LuaBitOp: %d independent record cases plus exponent/signed-zero intrinsics", count)
}
