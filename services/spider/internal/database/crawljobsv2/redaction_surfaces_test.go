package crawljobsv2

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"testing"
)

type redactionSurfaceCase struct {
	name      string
	typeName  string
	value     any
	composite bool
	rawValues []string
}

func TestSensitivePrimitiveRedactionSurfaces(t *testing.T) {
	fixture := newRedactionSurfaceFixture()
	tests := []redactionSurfaceCase{
		{name: "run ID", typeName: "RunID", value: fixture.runID, rawValues: []string{fixture.runRaw}},
		{name: "job ID", typeName: "JobID", value: fixture.jobID, rawValues: []string{fixture.jobRaw}},
		{name: "owner ID", typeName: "OwnerID", value: fixture.ownerID, rawValues: []string{fixture.ownerRaw}},
		{name: "lease token", typeName: "LeaseToken", value: fixture.token, rawValues: []string{fixture.tokenRaw}},
		{name: "digest and scope ID", typeName: "Digest", value: fixture.digest, rawValues: []string{fixture.digestRaw}},
		{name: "reservation ID", typeName: "ReservationID", value: fixture.reservationID, rawValues: []string{fixture.reservationRaw}},
		{name: "rate scope ID", typeName: "RateScopeID", value: fixture.rateScopeID, rawValues: []string{fixture.rateScopeRaw}},
		{name: "group ID", typeName: "GroupID", value: fixture.groupID, rawValues: []string{fixture.groupRaw}},
		{name: "canonical origin", typeName: "CanonicalOrigin", value: fixture.origin, rawValues: []string{fixture.originRaw}},
		{name: "image digest", typeName: "ImageDigest", value: ImageDigest("sha256:" + fixture.digestRaw), rawValues: []string{fixture.digestRaw}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertRedactionSurfaces(t, test)
		})
	}
}

func TestSensitiveCompositeRedactionSurfaces(t *testing.T) {
	fixture := newRedactionSurfaceFixture()
	binding := startRequestBinding{
		intent: ReservationIntent{
			Lease: fixture.lease, Target: fixture.target, CrawlPolicyDigest: fixture.digest, Decision: fixture.decision,
		},
		reservationID: fixture.reservationID,
		started:       fixture.started,
	}
	authorityState := newRequestIOAuthorityState(binding)
	runBinding := runPolicyBinding{
		runID: fixture.runID, crawlPolicySHA256: fixture.digest, renderPolicySHA256: fixture.digest,
		policyGroupCount: 7, policyGroupMapSHA256: fixture.digest,
	}
	runAuthorityState := runPolicyAuthorityState{
		binding: runBinding, recordSHA256: fixture.digest, integrity: fixture.digest,
	}
	compatibilityInput := CompatibilityArtifactInput{
		RedisConfigSHA256: fixture.digest, CommitGuardSHA256: fixture.digest,
		SpiderImage: ImageDigest("sha256:" + fixture.digestRaw), RenderWorkerImage: fixture.textRaw,
	}
	compatibility := CompatibilityArtifact{input: compatibilityInput, initialized: true}
	guardInput := GuardCoreInput{
		ContractSHA256: fixture.digest, RedisVersion: fixture.textRaw,
		RedisConfigSHA256: fixture.digest, MaximumShapeSHA256: fixture.digest,
		CandidateRunID: fixture.runID,
	}
	guard := GuardCore{input: guardInput, initialized: true}
	legacyInput := LegacyRetirementRecordInput{FreezeNonce: fixture.ownerRaw, BackupSHA256: fixture.digest}
	adminInput := AdminFreezeRecordInput{FreezeNonce: fixture.ownerRaw, ProcessStopEvidenceSHA256: fixture.digest}
	durabilityInput := DurabilityRecordInput{
		ApprovedRedisRunID: strings.Repeat("8", 40), BootEpoch: fixture.ownerRaw,
		PlannedShutdownNonce: fixture.ownerRaw, RehearsalEvidenceSHA256: fixture.digest,
	}
	tests := []redactionSurfaceCase{
		{name: "field", typeName: "Field", value: Field{Name: fixture.textRaw, Value: []byte(fixture.textRaw)}},
		{name: "record", typeName: "Record", value: Record{{Name: fixture.textRaw, Value: []byte(fixture.textRaw)}}},
		{name: "request target", typeName: "RequestTarget", value: fixture.target},
		{name: "lease identity", typeName: "LeaseIdentity", value: fixture.lease},
		{name: "policy decision", typeName: "PolicyDecision", value: fixture.decision},
		{name: "policy decision input", typeName: "PolicyDecisionInput", value: PolicyDecisionInput{Target: fixture.target, GroupID: fixture.groupID, RateScopeID: fixture.rateScopeID}},
		{name: "policy group", typeName: "PolicyGroup", value: PolicyGroup{GroupID: fixture.groupID, RateScopeID: fixture.rateScopeID, GroupScopeID: fixture.digest}},
		{name: "reservation intent", typeName: "ReservationIntent", value: ReservationIntent{Lease: fixture.lease, Target: fixture.target, CrawlPolicyDigest: fixture.digest, Decision: fixture.decision}},
		{name: "publication identity", typeName: "PublicationIdentity", value: PublicationIdentity{RunID: fixture.runID, JobID: fixture.jobID, OutputDigest: fixture.digest}},
		{name: "commit identity", typeName: "CommitIdentity", value: CommitIdentity{RunID: fixture.runID, JobID: fixture.jobID, OwnerID: fixture.ownerID, Token: fixture.token, PublicationID: fixture.digest, RequestStartsBaseline: 2, RequestStartsGeneration: 3}},
		{name: "source job", typeName: "SourceJob", value: fixture.source},
		{name: "output page", typeName: "OutputPage", value: fixture.page},
		{name: "output image", typeName: "OutputImage", value: OutputImage{NormalizedSourceURL: fixture.urlRaw, Alt: fixture.textRaw}},
		{name: "output discovery", typeName: "OutputDiscovery", value: fixture.discovery},
		{name: "output alias", typeName: "outputAlias", value: outputAlias{URLID: fixture.jobID, CanonicalURL: fixture.urlRaw}},
		{name: "successful document request", typeName: "SuccessfulDocumentRequest", value: SuccessfulDocumentRequest{target: fixture.target, lease: fixture.lease}},
		{name: "document transcript", typeName: "DocumentTranscript", value: DocumentTranscript{sourceJobID: fixture.jobID, sourceURL: fixture.urlRaw, lease: fixture.lease, requests: []SuccessfulDocumentRequest{{target: fixture.target, lease: fixture.lease}}}},
		{name: "final document witness", typeName: "FinalDocumentWitness", value: FinalDocumentWitness{lease: fixture.lease, target: fixture.target, targetDigest: fixture.digest, leaseRequestStartsBaseline: 2, terminalJobRequestStarts: 3}},
		{name: "run policy binding", typeName: "runPolicyBinding", value: runBinding},
		{name: "run policy authority state", typeName: "runPolicyAuthorityState", value: runAuthorityState},
		{name: "run policy authority", typeName: "RunPolicyAuthority", value: RunPolicyAuthority{binding: runBinding, state: &runAuthorityState}},
		{name: "render authorization", typeName: "RenderPolicyAuthorization", value: RenderPolicyAuthorization{runID: fixture.runID, digest: fixture.digest}},
		{name: "output context", typeName: "OutputContext", value: OutputContext{jobID: fixture.jobID, lease: fixture.lease, requestStartsBaseline: 2, requestStartsGeneration: 3, finalTarget: fixture.target, lastCrawled: fixture.textRaw, sourceRequest: &SuccessfulDocumentRequest{target: fixture.target, lease: fixture.lease, authority: authorityState}, witness: &FinalDocumentWitness{lease: fixture.lease, target: fixture.target, targetDigest: fixture.digest, redisStartedAtMS: 1001, terminalRequestStartedAtMS: 1002}, aliases: []outputAlias{{URLID: fixture.jobID, CanonicalURL: fixture.urlRaw}}}},
		{name: "crawl output", typeName: "CrawlOutput", value: CrawlOutput{Page: fixture.page, Outlinks: []string{fixture.urlRaw}, Discoveries: []OutputDiscovery{fixture.discovery}}},
		{name: "stage chunk", typeName: "StageChunk", value: StageChunk{commitID: fixture.digest, kind: ChunkHTML, context: OutputContext{lease: fixture.lease, requestStartsBaseline: 2, requestStartsGeneration: 3}, identity: CommitIdentity{Token: fixture.token, RequestStartsBaseline: 2, RequestStartsGeneration: 3}, records: []Record{{{Name: fixture.textRaw, Value: []byte(fixture.textRaw)}}}}},
		{name: "reject ready input", typeName: "RejectReadyTransitionInput", value: RejectReadyTransitionInput{RunID: fixture.runID, Job: fixture.source}},
		{name: "try claim input", typeName: "TryClaimTransitionInput", value: TryClaimTransitionInput{Job: fixture.source, Lease: fixture.lease}},
		{name: "release input", typeName: "ReleaseBeforeIOTransitionInput", value: ReleaseBeforeIOTransitionInput{Lease: fixture.lease}},
		{name: "retry input", typeName: "RetryTransitionInput", value: RetryTransitionInput{Lease: fixture.lease}},
		{name: "dead input", typeName: "DeadTransitionInput", value: DeadTransitionInput{Lease: fixture.lease}},
		{name: "cancel input", typeName: "CancelJobTransitionInput", value: CancelJobTransitionInput{Lease: fixture.lease}},
		{name: "complete input", typeName: "CompleteNoOutputTransitionInput", value: CompleteNoOutputTransitionInput{Lease: fixture.lease}},
		{name: "abort input", typeName: "AbortStageTransitionInput", value: AbortStageTransitionInput{Lease: fixture.lease, CommitID: fixture.digest}},
		{name: "request started", typeName: "StartRequestStarted", value: fixture.started},
		{name: "request rate blocked", typeName: "StartRequestRateBlocked", value: StartRequestRateBlocked{scopeID: fixture.digest}},
		{name: "lease lost response", typeName: "LeaseLostResponse", value: LeaseLostResponse{currentFence: 7}},
		{name: "start request response", typeName: "StartRequestResponse", value: StartRequestResponse{status: StatusStarted, started: &fixture.started, rateBlocked: &StartRequestRateBlocked{scopeID: fixture.digest}, authority: authorityState}},
		{name: "request I/O permit", typeName: "RequestIOPermit", value: RequestIOPermit{authority: authorityState, initialized: true}},
		{name: "parsed response", typeName: "parsedResponse", value: parsedResponse{status: StatusStarted, tail: []string{fixture.reservationRaw, fixture.urlRaw, fixture.textRaw}}},
		{name: "transport authority", typeName: "transportAuthority", value: newTestTransportAuthority()},
		{name: "start request binding", typeName: "startRequestBinding", value: binding},
		{name: "request authority state", typeName: "requestIOAuthorityState", value: *authorityState},
		{name: "transport gate input", typeName: "TransportGateInput", value: TransportGateInput{BootEpoch: fixture.ownerRaw, Contract: fixture.digest}},
		{name: "transport gate", typeName: "TransportGate", value: TransportGate{arguments: [7][]byte{[]byte(fixture.redisArgumentRaw), []byte(fixture.urlRaw)}}},
		{name: "evalsha request", typeName: "EvalSHARequest", value: EvalSHARequest{keys: [][]byte{[]byte(fixture.urlRaw)}, arguments: [][]byte{[]byte(fixture.redisArgumentRaw)}, sourceSHA256: fixture.digest}},
		{name: "compatibility artifact input", typeName: "CompatibilityArtifactInput", value: compatibilityInput},
		{name: "compatibility artifact", typeName: "CompatibilityArtifact", value: compatibility},
		{name: "compatibility marker", typeName: "CompatibilityMarker", value: CompatibilityMarker{artifact: compatibility, manifestSHA256: fixture.digest}},
		{name: "guard core input", typeName: "GuardCoreInput", value: guardInput},
		{name: "guard core", typeName: "GuardCore", value: guard},
		{name: "provisional guard core", typeName: "ProvisionalGuardCore", value: ProvisionalGuardCore{input: guardInput, initialized: true}},
		{name: "stored commit guard", typeName: "StoredCommitGuard", value: StoredCommitGuard{core: guard, compatibilityManifestSHA256: fixture.digest}},
		{name: "legacy retirement input", typeName: "LegacyRetirementRecordInput", value: legacyInput},
		{name: "legacy retirement record", typeName: "LegacyRetirementRecord", value: LegacyRetirementRecord{input: legacyInput, initialized: true}},
		{name: "admin freeze input", typeName: "AdminFreezeRecordInput", value: adminInput},
		{name: "admin freeze record", typeName: "AdminFreezeRecord", value: AdminFreezeRecord{input: adminInput, initialized: true}},
		{name: "durability input", typeName: "DurabilityRecordInput", value: durabilityInput},
		{name: "durability record", typeName: "DurabilityRecord", value: DurabilityRecord{input: durabilityInput, initialized: true}},
		{name: "first request evidence", typeName: "FirstRequestStartEvidence", value: FirstRequestStartEvidence{runID: fixture.runID, jobID: fixture.jobID}},
		{name: "final page record", typeName: "FinalPageRecord", value: FinalPageRecord{record: Record{{Name: fixture.textRaw, Value: []byte(fixture.htmlRaw)}}}},
		{name: "final image record", typeName: "FinalImageRecord", value: FinalImageRecord{publicationID: fixture.digest, normalizedPageURL: fixture.urlRaw, normalizedSourceURL: fixture.urlRaw, alt: fixture.textRaw}},
		{name: "image manifest record", typeName: "ImageManifestRecord", value: ImageManifestRecord{publicationID: fixture.digest, normalizedURL: fixture.urlRaw, imageKeys: []string{fixture.redisArgumentRaw}}},
	}

	for index := range tests {
		tests[index].composite = true
		tests[index].rawValues = fixture.allRawValues()
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertRedactionSurfaces(t, test)
		})
	}
}

func TestSensitivePrimitiveExplicitProtocolConversionsRemainRaw(t *testing.T) {
	fixture := newRedactionSurfaceFixture()
	tests := []struct {
		name string
		raw  string
		text string
		data []byte
	}{
		{name: "run ID", raw: fixture.runRaw, text: string(fixture.runID), data: []byte(fixture.runID)},
		{name: "job ID", raw: fixture.jobRaw, text: string(fixture.jobID), data: []byte(fixture.jobID)},
		{name: "owner ID", raw: fixture.ownerRaw, text: string(fixture.ownerID), data: []byte(fixture.ownerID)},
		{name: "lease token", raw: fixture.tokenRaw, text: string(fixture.token), data: []byte(fixture.token)},
		{name: "digest", raw: fixture.digestRaw, text: string(fixture.digest), data: []byte(fixture.digest)},
		{name: "reservation ID", raw: fixture.reservationRaw, text: string(fixture.reservationID), data: []byte(fixture.reservationID)},
		{name: "rate scope ID", raw: fixture.rateScopeRaw, text: string(fixture.rateScopeID), data: []byte(fixture.rateScopeID)},
		{name: "group ID", raw: fixture.groupRaw, text: string(fixture.groupID), data: []byte(fixture.groupID)},
		{name: "canonical origin", raw: fixture.originRaw, text: string(fixture.origin), data: []byte(fixture.origin)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.text != test.raw || !bytes.Equal(test.data, []byte(test.raw)) {
				t.Fatal("explicit protocol conversion changed identity bytes")
			}
			if !bytes.Equal(F(test.data), F([]byte(test.raw))) {
				t.Fatal("redaction changed explicit protocol framing")
			}
		})
	}
}

func assertRedactionSurfaces(t *testing.T, test redactionSurfaceCase) {
	t.Helper()
	wantRendered := redactedValue
	if test.composite {
		wantRendered = redactedString(test.typeName)
	}
	formats := []struct {
		format string
		want   string
	}{
		{format: "%v", want: wantRendered},
		{format: "%+v", want: wantRendered},
		{format: "%s", want: wantRendered},
		{format: "%q", want: strconv.Quote(wantRendered)},
		{format: "%#v", want: wantRendered},
		{format: "%x", want: wantRendered},
	}
	for _, format := range formats {
		formatted := fmt.Sprintf(format.format, test.value)
		if formatted != format.want {
			t.Fatalf("%T format %s did not produce the fixed redaction", test.value, format.format)
		}
		assertRawValuesAbsent(t, formatted, test.rawValues)
	}

	encoded, err := json.Marshal(test.value)
	if err != nil || !json.Valid(encoded) {
		t.Fatalf("marshal %T: %v", test.value, err)
	}
	wantJSON := `{"type":"crawljobsv2.` + test.typeName + `","redacted":true}`
	if string(encoded) != wantJSON {
		t.Fatalf("%T JSON did not produce the fixed redaction shape", test.value)
	}
	assertRawValuesAbsent(t, string(encoded), test.rawValues)

	textMarshaler, ok := test.value.(encoding.TextMarshaler)
	if !ok {
		t.Fatalf("%T does not implement encoding.TextMarshaler", test.value)
	}
	text, err := textMarshaler.MarshalText()
	if err != nil || string(text) != wantRendered {
		t.Fatalf("%T text marshaling did not produce the fixed redaction", test.value)
	}
	assertRawValuesAbsent(t, string(text), test.rawValues)

	logValuer, ok := test.value.(slog.LogValuer)
	if !ok {
		t.Fatalf("%T does not implement slog.LogValuer", test.value)
	}
	logValue := logValuer.LogValue().Resolve()
	if logValue.Kind() != slog.KindGroup {
		t.Fatalf("%T slog value kind = %s, want group", test.value, logValue.Kind())
	}
	var foundType, foundRedacted bool
	for _, attribute := range logValue.Group() {
		switch attribute.Key {
		case "type":
			foundType = attribute.Value.Kind() == slog.KindString && attribute.Value.String() == "crawljobsv2."+test.typeName
		case "redacted":
			foundRedacted = attribute.Value.Kind() == slog.KindBool && attribute.Value.Bool()
		}
	}
	if !foundType || !foundRedacted {
		t.Fatalf("%T slog value did not produce the fixed redaction shape", test.value)
	}

	structuredHandlers := []struct {
		name string
		new  func(*bytes.Buffer) slog.Handler
	}{
		{name: "JSON handler", new: func(buffer *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(buffer, nil) }},
		{name: "text handler", new: func(buffer *bytes.Buffer) slog.Handler { return slog.NewTextHandler(buffer, nil) }},
	}
	for _, handler := range structuredHandlers {
		var buffer bytes.Buffer
		slog.New(handler.new(&buffer)).Info("redaction_test", "value", test.value)
		logged := buffer.String()
		assertRawValuesAbsent(t, logged, test.rawValues)
		if !strings.Contains(logged, "crawljobsv2."+test.typeName) || !strings.Contains(logged, "redacted") || !strings.Contains(logged, "true") {
			t.Fatalf("%T %s omitted the fixed redaction shape", test.value, handler.name)
		}
	}
}

func assertRawValuesAbsent(t *testing.T, output string, rawValues []string) {
	t.Helper()
	for _, rawValue := range rawValues {
		if rawValue != "" && strings.Contains(output, rawValue) {
			t.Fatal("redaction surface exposed a raw identity or value")
		}
	}
}

type redactionSurfaceFixture struct {
	runRaw           string
	jobRaw           string
	ownerRaw         string
	tokenRaw         string
	digestRaw        string
	reservationRaw   string
	rateScopeRaw     string
	groupRaw         string
	originRaw        string
	urlRaw           string
	textRaw          string
	htmlRaw          string
	redisArgumentRaw string
	runID            RunID
	jobID            JobID
	ownerID          OwnerID
	token            LeaseToken
	digest           Digest
	reservationID    ReservationID
	rateScopeID      RateScopeID
	groupID          GroupID
	origin           CanonicalOrigin
	target           RequestTarget
	lease            LeaseIdentity
	decision         PolicyDecision
	source           SourceJob
	page             OutputPage
	discovery        OutputDiscovery
	started          StartRequestStarted
}

func newRedactionSurfaceFixture() redactionSurfaceFixture {
	fixture := redactionSurfaceFixture{
		runRaw:           strings.Repeat("1", 32),
		jobRaw:           strings.Repeat("2", 64),
		ownerRaw:         strings.Repeat("3", 32),
		tokenRaw:         strings.Repeat("4", 64),
		digestRaw:        strings.Repeat("5", 64),
		reservationRaw:   strings.Repeat("6", 64),
		rateScopeRaw:     strings.Repeat("7", 32),
		groupRaw:         "GROUP_IDENTITY_REDACTION_CANARY",
		originRaw:        "https://origin-redaction-canary.example:443",
		urlRaw:           "https://url-redaction-canary.example/private",
		textRaw:          "ARBITRARY_VALUE_REDACTION_CANARY",
		htmlRaw:          "<html>HTML_REDACTION_CANARY</html>",
		redisArgumentRaw: "REDIS_ARGUMENT_REDACTION_CANARY",
	}
	fixture.runID = RunID(fixture.runRaw)
	fixture.jobID = JobID(fixture.jobRaw)
	fixture.ownerID = OwnerID(fixture.ownerRaw)
	fixture.token = LeaseToken(fixture.tokenRaw)
	fixture.digest = Digest(fixture.digestRaw)
	fixture.reservationID = ReservationID(fixture.reservationRaw)
	fixture.rateScopeID = RateScopeID(fixture.rateScopeRaw)
	fixture.groupID = GroupID(fixture.groupRaw)
	fixture.origin = CanonicalOrigin(fixture.originRaw)
	fixture.target = RequestTarget{URLID: fixture.jobID, CanonicalURL: fixture.urlRaw}
	fixture.lease = LeaseIdentity{
		RunID: fixture.runID, JobID: fixture.jobID, OwnerID: fixture.ownerID, Fence: 7, Token: fixture.token,
	}
	fixture.decision = PolicyDecision{
		RequestKind: RequestDocument, TargetURLID: fixture.jobID, TargetDigest: fixture.digest,
		GroupID: fixture.groupID, RateScopeID: fixture.rateScopeID,
		GlobalScopeID: fixture.digest, GroupScopeID: fixture.digest, OriginScopeID: fixture.digest,
	}
	fixture.source = SourceJob{
		JobID: fixture.jobID, CanonicalURL: fixture.urlRaw, GroupID: fixture.groupID,
		RateScopeID: fixture.rateScopeID, Decision: fixture.decision,
	}
	fixture.page = OutputPage{
		NormalizedURL: fixture.urlRaw, HTML: []byte(fixture.textRaw), ContentType: fixture.textRaw,
		RenderPolicyDigest: fixture.digest,
	}
	fixture.discovery = OutputDiscovery{
		JobID: fixture.jobID, CanonicalURL: fixture.urlRaw, GroupID: fixture.groupID,
		RateScopeID: fixture.rateScopeID, Decision: fixture.decision,
	}
	fixture.started = StartRequestStarted{reservationID: fixture.reservationID}
	return fixture
}

func (fixture redactionSurfaceFixture) allRawValues() []string {
	return []string{
		fixture.runRaw,
		fixture.jobRaw,
		fixture.ownerRaw,
		fixture.tokenRaw,
		fixture.digestRaw,
		fixture.reservationRaw,
		fixture.rateScopeRaw,
		fixture.groupRaw,
		fixture.originRaw,
		fixture.urlRaw,
		fixture.textRaw,
		fixture.htmlRaw,
		fixture.redisArgumentRaw,
	}
}
