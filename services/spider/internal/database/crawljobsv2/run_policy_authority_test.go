package crawljobsv2

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	"github.com/IonelPopJara/search-engine/services/spider/internal/renderpolicy"
)

func newAuthenticatedTestRunPolicyAuthority(
	t *testing.T,
	runID RunID,
	crawlPolicySHA256 Digest,
	renderPolicySHA256 Digest,
	groups []PolicyGroup,
) RunPolicyAuthority {
	t.Helper()
	policyGroupMapSHA256, err := DerivePolicyGroupMapDigest(groups)
	if err != nil {
		t.Fatalf("derive test policy-group map: %v", err)
	}
	record := recordAuthorityRunRecord(t, "loading")
	record[runCrawlPolicySHA256Index].Value = []byte(crawlPolicySHA256)
	record[runRenderPolicySHA256Index].Value = []byte(renderPolicySHA256)
	record[runPolicyGroupCountIndex].Value = []byte(canonicalDecimal(uint64(len(groups))))
	record[runPolicyGroupMapSHA256Index].Value = []byte(policyGroupMapSHA256)
	authority, err := newTestTransportAuthority().parseRunPolicyAuthority(runID, record, groups)
	if err != nil {
		t.Fatalf("parse test run-policy authority: %v", err)
	}
	return authority
}

func testDenyAllRenderPolicyArtifact() []byte {
	return []byte(`{"schema_version":1,"default_action":"deny","rules":[]}`)
}

func testScopedRenderPolicyArtifact(
	t *testing.T,
	ruleID string,
	enabled bool,
	host string,
	allowPaths []string,
	allowPathPrefixes []string,
	denyPathPrefixes []string,
) []byte {
	t.Helper()
	rule := map[string]any{
		"id":                  ruleID,
		"enabled":             enabled,
		"host_rule":           map[string]any{"host": host, "match": "exact"},
		"allow_paths":         append([]string{}, allowPaths...),
		"allow_path_prefixes": append([]string{}, allowPathPrefixes...),
		"deny_path_prefixes":  append([]string{}, denyPathPrefixes...),
		"mode":                "inline_only",
		"failure_action":      "reject_page",
		"resource_rules":      []any{},
		"network_controls": map[string]any{
			"allowed_methods":             []string{"GET"},
			"robots_for_resources":        true,
			"allow_cookies":               false,
			"allow_service_workers":       false,
			"allow_websockets":            false,
			"allow_webrtc":                false,
			"allow_downloads":             false,
			"allow_popups":                false,
			"allow_secondary_documents":   false,
			"allow_javascript_navigation": false,
		},
		"limits": map[string]any{
			"max_render_time_ms":           1000,
			"settle_time_ms":               0,
			"max_resource_requests":        0,
			"max_aggregate_resource_bytes": 0,
			"max_resource_body_bytes":      0,
			"max_rendered_dom_bytes":       1048576,
			"max_dom_nodes":                1000,
			"max_redirect_hops":            0,
			"max_console_bytes":            0,
		},
	}
	document := map[string]any{
		"schema_version": 1,
		"default_action": "deny",
		"rules":          []any{rule},
	}
	artifact, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal test render policy: %v", err)
	}
	return artifact
}

func testRenderPolicyArtifactForTarget(t *testing.T, ruleID string, enabled bool, target RequestTarget) []byte {
	t.Helper()
	parsed, err := url.Parse(target.CanonicalURL)
	if err != nil || parsed.Hostname() == "" || parsed.EscapedPath() == "" {
		t.Fatalf("parse render-policy target: %v", err)
	}
	return testScopedRenderPolicyArtifact(t, ruleID, enabled, parsed.Hostname(), []string{parsed.EscapedPath()}, nil, nil)
}

// newNonAuthoritativeDigestVectorRenderProjection exists only for historical
// digest-serialization vectors that pin a synthetic digest with no artifact
// preimage. It still uses a transport-authenticated test run snapshot and a
// strictly decoded immutable matcher, but deliberately does not test the
// production artifact/digest join. All final-gate tests use
// NewRenderPolicyAuthorization instead.
func newNonAuthoritativeDigestVectorRenderProjection(
	t *testing.T,
	runPolicy RunPolicyAuthority,
	exactMatcherArtifact []byte,
) RenderPolicyAuthorization {
	t.Helper()
	binding, err := runPolicy.authenticatedBinding()
	if err != nil {
		t.Fatal(err)
	}
	artifact := append([]byte(nil), exactMatcherArtifact...)
	matcher, err := renderpolicy.Decode(bytes.NewReader(artifact))
	if err != nil {
		t.Fatalf("decode historical vector matcher: %v", err)
	}
	matcherSHA256, err := renderPolicyMatcherDigest(matcher)
	if err != nil {
		t.Fatal(err)
	}
	return RenderPolicyAuthorization{
		runID: binding.runID, digest: binding.renderPolicySHA256, matcher: matcher,
		matcherSHA256: matcherSHA256, runPolicyAuthority: runPolicy,
		seal: &decodedRenderPolicyAuthorizationSeal, initialized: true,
	}
}

func TestRunPolicyAuthorityRequiresValidatedTransportRunSnapshot(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	runID := vectorLease(t, fixture).RunID
	groups := vectorPolicyGroupsValue(t, fixture)
	crawlPolicySHA256 := mustFixtureDigest(t, fixture.Identities.CrawlPolicyDigest)
	renderArtifact := testDenyAllRenderPolicyArtifact()
	renderPolicySHA256 := plainSHA256(renderArtifact)

	record := recordAuthorityRunRecord(t, "loading")
	groupMapSHA256, err := DerivePolicyGroupMapDigest(groups)
	if err != nil {
		t.Fatal(err)
	}
	record[runCrawlPolicySHA256Index].Value = []byte(crawlPolicySHA256)
	record[runRenderPolicySHA256Index].Value = []byte(renderPolicySHA256)
	record[runPolicyGroupCountIndex].Value = []byte(canonicalDecimal(uint64(len(groups))))
	record[runPolicyGroupMapSHA256Index].Value = []byte(groupMapSHA256)

	authority, err := newTestTransportAuthority().parseRunPolicyAuthority(runID, record, groups)
	if err != nil {
		t.Fatalf("valid run authority: %v", err)
	}
	binding, err := authority.authenticatedBinding()
	if err != nil {
		t.Fatal(err)
	}
	if binding.runID != runID || binding.crawlPolicySHA256 != crawlPolicySHA256 ||
		binding.renderPolicySHA256 != renderPolicySHA256 || binding.policyGroupCount != uint64(len(groups)) ||
		binding.policyGroupMapSHA256 != groupMapSHA256 {
		t.Fatal("run authority lost an authenticated run field")
	}

	// The authority owns both snapshots, not caller-owned record or group storage.
	record[runCrawlPolicySHA256Index].Value[0] = 'f'
	groups[0].Concurrency++
	if _, err := authority.authenticatedBinding(); err != nil {
		t.Fatalf("caller input mutation changed authority: %v", err)
	}

	if _, err := (RunPolicyAuthority{}).authenticatedBinding(); !errors.Is(err, ErrInvalidResponseAuthority) {
		t.Fatalf("zero authority error = %v", err)
	}
	copyAuthority := authority
	if _, err := copyAuthority.authenticatedBinding(); err != nil {
		t.Fatalf("safe authority copy rejected: %v", err)
	}
	tampered := authority
	tampered.binding.renderPolicySHA256 = Digest(strings.Repeat("f", 64))
	if _, err := tampered.authenticatedBinding(); !errors.Is(err, ErrInvalidResponseAuthority) {
		t.Fatalf("tampered copy error = %v", err)
	}
	tamperedState := authority
	stateCopy := *authority.state
	stateCopy.recordSHA256 = Digest(strings.Repeat("e", 64))
	tamperedState.state = &stateCopy
	if _, err := tamperedState.authenticatedBinding(); !errors.Is(err, ErrInvalidResponseAuthority) {
		t.Fatalf("tampered copied state error = %v", err)
	}
	tamperedMap := authority
	mapStateCopy := *authority.state
	mapStateCopy.policyGroups = clonePolicyGroupMap(authority.state.policyGroups)
	for groupID, group := range mapStateCopy.policyGroups {
		group.Concurrency++
		mapStateCopy.policyGroups[groupID] = group
		break
	}
	tamperedMap.state = &mapStateCopy
	if _, err := tamperedMap.authenticatedBinding(); !errors.Is(err, ErrInvalidResponseAuthority) {
		t.Fatalf("tampered policy-group map error = %v", err)
	}

	invalidRecord := cloneRecord(recordAuthorityRunRecord(t, "loading"))
	invalidRecord[runPolicyGroupCountIndex].Value = []byte("0")
	if _, err := newTestTransportAuthority().parseRunPolicyAuthority(runID, invalidRecord, groups); err == nil {
		t.Fatal("invalid SchemaRun record produced authority")
	}
	validRecord := recordAuthorityRunRecord(t, "loading")
	validRecord[runCrawlPolicySHA256Index].Value = []byte(crawlPolicySHA256)
	validRecord[runRenderPolicySHA256Index].Value = []byte(renderPolicySHA256)
	validRecord[runPolicyGroupCountIndex].Value = []byte(canonicalDecimal(uint64(len(authority.state.policyGroups))))
	validRecord[runPolicyGroupMapSHA256Index].Value = []byte(groupMapSHA256)
	wrongGroups := policyGroupsFromMap(authority.state.policyGroups)
	wrongGroups[0].IntervalMS++
	if _, err := newTestTransportAuthority().parseRunPolicyAuthority(runID, validRecord, wrongGroups); !errors.Is(err, ErrPolicyGroupBindingMismatch) {
		t.Fatalf("run/groups digest mismatch error = %v", err)
	}
	if _, err := newTestTransportAuthority().parseRunPolicyAuthority(runID, validRecord, wrongGroups[:len(wrongGroups)-1]); !errors.Is(err, ErrPolicyGroupBindingMismatch) {
		t.Fatalf("run/groups count mismatch error = %v", err)
	}
	if _, err := (transportAuthority{}).parseRunPolicyAuthority(runID, validRecord, policyGroupsFromMap(authority.state.policyGroups)); !errors.Is(err, ErrInvalidResponseAuthority) {
		t.Fatalf("unsealed transport error = %v", err)
	}
}

func TestRunPolicyAuthorityParserRejectsZeroSHA256(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	runID := vectorLease(t, fixture).RunID
	groups := vectorPolicyGroupsValue(t, fixture)
	crawlPolicySHA256 := mustFixtureDigest(t, fixture.Identities.CrawlPolicyDigest)
	renderPolicySHA256 := plainSHA256(testDenyAllRenderPolicyArtifact())
	policyGroupMapSHA256, err := DerivePolicyGroupMapDigest(groups)
	if err != nil {
		t.Fatal(err)
	}
	valid := recordAuthorityRunRecord(t, "loading")
	valid[runCrawlPolicySHA256Index].Value = []byte(crawlPolicySHA256)
	valid[runRenderPolicySHA256Index].Value = []byte(renderPolicySHA256)
	valid[runPolicyGroupCountIndex].Value = []byte(canonicalDecimal(uint64(len(groups))))
	valid[runPolicyGroupMapSHA256Index].Value = []byte(policyGroupMapSHA256)
	if _, err := newTestTransportAuthority().parseRunPolicyAuthority(runID, valid, groups); err != nil {
		t.Fatalf("valid run-policy authority fixture: %v", err)
	}

	tests := []struct {
		name   string
		index  int
		groups func([]PolicyGroup) []PolicyGroup
	}{
		{name: "crawl policy", index: runCrawlPolicySHA256Index},
		{name: "render policy", index: runRenderPolicySHA256Index},
		{name: "policy-group map", index: runPolicyGroupMapSHA256Index},
		{
			name:  "policy-group scope",
			index: -1,
			groups: func(values []PolicyGroup) []PolicyGroup {
				values[0].GroupScopeID = Digest(ZeroSHA256)
				return values
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			record := cloneRecord(valid)
			candidateGroups := append([]PolicyGroup(nil), groups...)
			if testCase.index >= 0 {
				record[testCase.index].Value = []byte(ZeroSHA256)
			} else {
				candidateGroups = testCase.groups(candidateGroups)
			}
			if _, err := newTestTransportAuthority().parseRunPolicyAuthority(runID, record, candidateGroups); err == nil {
				t.Fatal("transport parser produced run-policy authority from ZERO_SHA256")
			}
		})
	}
}

func TestRunPolicyAuthorityRedactsEveryGenericSurface(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	groups := vectorPolicyGroupsValue(t, fixture)
	artifact := testDenyAllRenderPolicyArtifact()
	authority := newAuthenticatedTestRunPolicyAuthority(
		t,
		vectorLease(t, fixture).RunID,
		mustFixtureDigest(t, fixture.Identities.CrawlPolicyDigest),
		plainSHA256(artifact),
		groups,
	)
	binding, err := authority.authenticatedBinding()
	if err != nil {
		t.Fatal(err)
	}

	outputs := []string{
		fmt.Sprint(authority),
		fmt.Sprintf("%v", authority),
		fmt.Sprintf("%+v", authority),
		fmt.Sprintf("%#v", authority),
		fmt.Sprintf("%q", authority),
	}
	jsonValue, err := json.Marshal(authority)
	if err != nil {
		t.Fatal(err)
	}
	outputs = append(outputs, string(jsonValue))
	textValue, err := authority.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	outputs = append(outputs, string(textValue), authority.LogValue().String())
	var logBuffer strings.Builder
	slog.New(slog.NewTextHandler(&logBuffer, nil)).Info("authority", "value", authority)
	outputs = append(outputs, logBuffer.String())

	secrets := []string{
		string(binding.runID), string(binding.crawlPolicySHA256), string(binding.renderPolicySHA256),
		string(binding.policyGroupMapSHA256),
	}
	for _, group := range groups {
		secrets = append(secrets, string(group.GroupID), string(group.RateScopeID), string(group.GroupScopeID))
	}
	for _, output := range outputs {
		if !strings.Contains(output, "redact") {
			t.Fatalf("surface is not visibly redacted: %q", output)
		}
		for _, secret := range secrets {
			if secret != "" && strings.Contains(output, secret) {
				t.Fatalf("surface disclosed authority field %q in %q", secret, output)
			}
		}
	}
}

func TestRunPinnedPolicyBindingsRequireAuthenticatedExactGroupMap(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	groups := vectorPolicyGroupsValue(t, fixture)
	authority := vectorRunPolicyAuthority(t, fixture, plainSHA256(testDenyAllRenderPolicyArtifact()))
	decision := vectorDecisionValue(t, fixture, "page_document")
	bindings := RunPinnedPolicyBindings{Decisions: []PolicyDecision{decision}}
	if err := ValidateRunPinnedPolicyBindings(authority, bindings); err != nil {
		t.Fatalf("authenticated exact group map: %v", err)
	}

	if err := ValidateRunPinnedPolicyBindings(RunPolicyAuthority{}, bindings); !errors.Is(err, ErrInvalidResponseAuthority) {
		t.Fatalf("zero run authority error = %v", err)
	}

	wrongTuple := bindings
	wrongTuple.Decisions = append([]PolicyDecision(nil), bindings.Decisions...)
	wrongTuple.Decisions[0].GroupConcurrency++
	wrongTuple.Decisions[0].OriginConcurrency++
	if err := ValidateRunPinnedPolicyBindings(authority, wrongTuple); !errors.Is(err, ErrPolicyGroupBindingMismatch) {
		t.Fatalf("authenticated policy-group tuple mismatch error = %v", err)
	}

	// This standalone helper remains useful before run creation, but passing it
	// cannot turn caller-selected groups into run authority.
	if err := ValidatePreRunPolicyDecisionGroupBinding(decision, groups); err != nil {
		t.Fatalf("standalone pre-run check: %v", err)
	}
	otherGroups := append([]PolicyGroup(nil), groups...)
	for index := range otherGroups {
		if otherGroups[index].GroupID == decision.GroupID {
			otherGroups[index].IntervalMS++
		}
	}
	otherAuthority := newAuthenticatedTestRunPolicyAuthority(
		t,
		vectorLease(t, fixture).RunID,
		mustFixtureDigest(t, fixture.Identities.CrawlPolicyDigest),
		plainSHA256(testDenyAllRenderPolicyArtifact()),
		otherGroups,
	)
	if err := ValidateRunPinnedPolicyBindings(otherAuthority, bindings); !errors.Is(err, ErrPolicyGroupBindingMismatch) {
		t.Fatalf("different authenticated map error = %v", err)
	}
}

func TestRunPolicyConsumersRejectPostValidationTupleMutation(t *testing.T) {
	fixture := newWireOracleFixture(t)
	binding, err := fixture.runPolicy.authenticatedBinding()
	if err != nil {
		t.Fatal(err)
	}

	jobs := []SourceJob{fixture.job}
	if err := ValidateRunPinnedPolicyBindings(fixture.runPolicy, RunPinnedPolicyBindings{SourceJobs: jobs}); err != nil {
		t.Fatalf("valid source precheck: %v", err)
	}
	jobs[0].Decision = mutateRunPolicyTuple(jobs[0].Decision)
	if _, err := DeriveSourceDigest(jobs); err != nil {
		t.Fatalf("mutated source must remain structurally valid: %v", err)
	}

	document := newTestSuccessfulStartEvent(
		t, fixture.runPolicy, fixture.job, fixture.lease, binding.crawlPolicySHA256,
		RequestDocument, fixture.intent.Target, 1, 1_788_266_090_000, 1, 1, 1,
	)
	sourceConsumers := []struct {
		name string
		call func() error
	}{
		{name: "aggregate validator", call: func() error {
			return ValidateRunPinnedPolicyBindings(fixture.runPolicy, RunPinnedPolicyBindings{SourceJobs: jobs})
		}},
		{name: "reject transition digest", call: func() error {
			_, err := DeriveRejectReadyTransitionID(fixture.runPolicy, RejectReadyTransitionInput{
				RunID: fixture.runID, Job: jobs[0], Reason: ReasonPolicyDenied,
			})
			return err
		}},
		{name: "enqueue wire", call: func() error {
			gate := wireOracleGate(t, fixture, OperationEnqueueBatch, wireOracleActive)
			_, err := NewEnqueueBatchWireRequest(gate, fixture.runPolicy, fixture.runID, jobs)
			return err
		}},
		{name: "audit wire", call: func() error {
			gate := wireOracleGate(t, fixture, OperationAuditRunBatch, wireOracleActive)
			_, err := NewAuditRunBatchWireRequest(gate, fixture.runPolicy, AuditRunBatchWireInput{RunID: fixture.runID, Jobs: jobs})
			return err
		}},
		{name: "reject wire", call: func() error {
			gate := wireOracleGate(t, fixture, OperationRejectReady, wireOracleActive)
			_, err := NewRejectReadyWireRequest(gate, fixture.runPolicy, RejectReadyTransitionInput{
				RunID: fixture.runID, Job: jobs[0], Reason: ReasonPolicyDenied,
			})
			return err
		}},
		{name: "request transcript", call: func() error {
			_, err := NewRequestTranscript(fixture.runPolicy, jobs[0], document)
			return err
		}},
		{name: "document transcript", call: func() error {
			_, err := NewDocumentTranscript(fixture.runPolicy, jobs[0], document)
			return err
		}},
	}
	for _, consumer := range sourceConsumers {
		t.Run("source/"+consumer.name, func(t *testing.T) {
			if err := consumer.call(); !errors.Is(err, ErrPolicyGroupBindingMismatch) {
				t.Fatalf("post-validation source mutation error = %v", err)
			}
		})
	}
	transcript, err := NewDocumentTranscript(fixture.runPolicy, fixture.job, document)
	if err != nil {
		t.Fatalf("valid transcript for append boundary: %v", err)
	}
	redirectTarget := wireOracleTarget(t, "https://example.com/policy-mutation-redirect")
	redirect := newTestSuccessfulStartEvent(
		t, fixture.runPolicy, fixture.job, fixture.lease, binding.crawlPolicySHA256,
		RequestRedirect, redirectTarget, 2, 1_788_266_091_000, 2, 2, 2,
	)
	tamperedRequestState := *redirect.authority
	tamperedRequestState.binding.intent.Decision = mutateRunPolicyTuple(tamperedRequestState.binding.intent.Decision)
	redirect.authority = &tamperedRequestState
	if _, err := transcript.AppendSuccessfulRequest(redirect); !errors.Is(err, ErrPolicyGroupBindingMismatch) {
		t.Fatalf("append boundary policy mutation error = %v", err)
	}

	intent := fixture.intent
	if _, err := DeriveReservationID(fixture.runPolicy, intent); err != nil {
		t.Fatalf("valid reservation precheck: %v", err)
	}
	intent.Decision = mutateRunPolicyTuple(intent.Decision)
	mutatedJob := fixture.job
	mutatedJob.Decision = intent.Decision
	if _, err := deriveReservationIDNonAuthoritative(intent); err != nil {
		t.Fatalf("mutated reservation must remain structurally valid: %v", err)
	}
	if _, err := completeSourceJobRecord(mutatedJob); err != nil {
		t.Fatalf("mutated claim source must remain structurally valid: %v", err)
	}
	claim := TryClaimTransitionInput{
		Job: mutatedJob, Lease: fixture.lease, ExpectedPriorFence: 1, InitialIntent: intent,
	}
	reservationConsumers := []struct {
		name string
		call func() error
	}{
		{name: "aggregate validator", call: func() error {
			return ValidateRunPinnedPolicyBindings(fixture.runPolicy, RunPinnedPolicyBindings{Decisions: []PolicyDecision{intent.Decision}})
		}},
		{name: "reservation digest", call: func() error {
			_, err := DeriveReservationID(fixture.runPolicy, intent)
			return err
		}},
		{name: "start response parser", call: func() error {
			_, err := newTestTransportAuthority().parseStartRequestResponse(fixture.runPolicy, intent, nil)
			return err
		}},
		{name: "claim transition digest", call: func() error {
			_, err := DeriveTryClaimTransitionID(fixture.runPolicy, claim)
			return err
		}},
		{name: "claim wire", call: func() error {
			gate := wireOracleGate(t, fixture, OperationTryClaim, wireOracleActive)
			_, err := NewTryClaimWireRequest(gate, fixture.runPolicy, claim)
			return err
		}},
		{name: "reserve wire", call: func() error {
			gate := wireOracleGate(t, fixture, OperationReserveRequest, wireOracleActive)
			_, err := NewReserveRequestWireRequest(gate, fixture.runPolicy, intent)
			return err
		}},
		{name: "start wire", call: func() error {
			gate := wireOracleGate(t, fixture, OperationStartRequest, wireOracleActive)
			_, err := NewStartRequestWireRequest(gate, fixture.runPolicy, intent)
			return err
		}},
		{name: "finish wire", call: func() error {
			gate := wireOracleGate(t, fixture, OperationFinishRequest, wireOracleActive)
			_, err := NewFinishRequestWireRequest(gate, fixture.runPolicy, intent)
			return err
		}},
		{name: "cancel wire", call: func() error {
			gate := wireOracleGate(t, fixture, OperationCancelReservation, wireOracleActive)
			_, err := NewCancelReservationWireRequest(gate, fixture.runPolicy, intent)
			return err
		}},
	}
	for _, consumer := range reservationConsumers {
		t.Run("reservation/"+consumer.name, func(t *testing.T) {
			if err := consumer.call(); !errors.Is(err, ErrPolicyGroupBindingMismatch) {
				t.Fatalf("post-validation reservation mutation error = %v", err)
			}
		})
	}
}

func TestSuccessfulRequestConsumerRevalidatesOpaqueIntent(t *testing.T) {
	fixture := newWireOracleFixture(t)
	response, err := newTestTransportAuthority().parseStartRequestResponse(fixture.runPolicy, fixture.intent, []string{
		string(StatusStarted), "1788266090000", string(fixture.reservationID), "1788266090000",
		"1", "1", "1", "1", "1",
	})
	if err != nil {
		t.Fatalf("valid start response: %v", err)
	}
	tamperedState := *response.authority
	tamperedState.binding.intent.Decision = mutateRunPolicyTuple(tamperedState.binding.intent.Decision)
	response.authority = &tamperedState
	permit, err := response.IOPermit()
	if err != nil {
		t.Fatalf("issue opaque test permit: %v", err)
	}
	if _, err := NewSuccessfulRequest(permit); !errors.Is(err, ErrPolicyGroupBindingMismatch) {
		t.Fatalf("successful-request boundary policy mutation error = %v", err)
	}
}

func TestRunPolicyDiscoveryConsumersRejectSliceMutation(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	context := vectorOutputContextValue(t, fixture)
	output := vectorOutputValue(t, fixture)
	discoveries := append([]OutputDiscovery(nil), output.Discoveries...)
	if err := ValidateRunPinnedPolicyBindings(context.runPolicy, RunPinnedPolicyBindings{Discoveries: discoveries}); err != nil {
		t.Fatalf("valid discovery precheck: %v", err)
	}
	discoveries[0].Decision = mutateRunPolicyTuple(discoveries[0].Decision)
	if _, err := completeDiscoveryRecord(discoveries[0]); err != nil {
		t.Fatalf("mutated discovery must remain structurally valid: %v", err)
	}
	output.Discoveries = discoveries
	identity := outputCommitIdentity(context, mustFixtureDigest(t, fixture.Expected.PublicationID))

	consumers := []struct {
		name string
		call func() error
	}{
		{name: "aggregate validator", call: func() error {
			return ValidateRunPinnedPolicyBindings(context.runPolicy, RunPinnedPolicyBindings{Discoveries: discoveries})
		}},
		{name: "output digest", call: func() error {
			_, err := DeriveOutputDigest(context, output)
			return err
		}},
		{name: "discoveries chunk", call: func() error {
			_, err := NewDiscoveriesStageChunk(identity, 0, context, discoveries)
			return err
		}},
	}
	for _, consumer := range consumers {
		t.Run(consumer.name, func(t *testing.T) {
			if err := consumer.call(); !errors.Is(err, ErrPolicyGroupBindingMismatch) {
				t.Fatalf("post-validation discovery mutation error = %v", err)
			}
		})
	}
}

func TestRunPolicyConsumersRejectCrossRunAuthority(t *testing.T) {
	fixture := newWireOracleFixture(t)
	binding, err := fixture.runPolicy.authenticatedBinding()
	if err != nil {
		t.Fatal(err)
	}
	otherRunPolicy := newAuthenticatedTestRunPolicyAuthority(
		t, fixture.otherRunID, binding.crawlPolicySHA256, binding.renderPolicySHA256, []PolicyGroup{fixture.policyGroup},
	)
	document := newTestSuccessfulStartEvent(
		t, fixture.runPolicy, fixture.job, fixture.lease, binding.crawlPolicySHA256,
		RequestDocument, fixture.intent.Target, 1, 1_788_266_090_000, 1, 1, 1,
	)
	claim := TryClaimTransitionInput{
		Job: fixture.job, Lease: fixture.lease, ExpectedPriorFence: 1, InitialIntent: fixture.intent,
	}

	consumers := []struct {
		name string
		call func() error
	}{
		{name: "reservation digest", call: func() error {
			_, err := DeriveReservationID(otherRunPolicy, fixture.intent)
			return err
		}},
		{name: "start response parser", call: func() error {
			_, err := newTestTransportAuthority().parseStartRequestResponse(otherRunPolicy, fixture.intent, nil)
			return err
		}},
		{name: "reject transition digest", call: func() error {
			_, err := DeriveRejectReadyTransitionID(otherRunPolicy, RejectReadyTransitionInput{
				RunID: fixture.runID, Job: fixture.job, Reason: ReasonPolicyDenied,
			})
			return err
		}},
		{name: "claim transition digest", call: func() error {
			_, err := DeriveTryClaimTransitionID(otherRunPolicy, claim)
			return err
		}},
		{name: "enqueue wire", call: func() error {
			gate := wireOracleGate(t, fixture, OperationEnqueueBatch, wireOracleActive)
			_, err := NewEnqueueBatchWireRequest(gate, otherRunPolicy, fixture.runID, []SourceJob{fixture.job})
			return err
		}},
		{name: "audit wire", call: func() error {
			gate := wireOracleGate(t, fixture, OperationAuditRunBatch, wireOracleActive)
			_, err := NewAuditRunBatchWireRequest(gate, otherRunPolicy, AuditRunBatchWireInput{RunID: fixture.runID, Jobs: []SourceJob{fixture.job}})
			return err
		}},
		{name: "reject wire", call: func() error {
			gate := wireOracleGate(t, fixture, OperationRejectReady, wireOracleActive)
			_, err := NewRejectReadyWireRequest(gate, otherRunPolicy, RejectReadyTransitionInput{
				RunID: fixture.runID, Job: fixture.job, Reason: ReasonPolicyDenied,
			})
			return err
		}},
		{name: "claim wire", call: func() error {
			gate := wireOracleGate(t, fixture, OperationTryClaim, wireOracleActive)
			_, err := NewTryClaimWireRequest(gate, otherRunPolicy, claim)
			return err
		}},
		{name: "reserve wire", call: func() error {
			gate := wireOracleGate(t, fixture, OperationReserveRequest, wireOracleActive)
			_, err := NewReserveRequestWireRequest(gate, otherRunPolicy, fixture.intent)
			return err
		}},
		{name: "start wire", call: func() error {
			gate := wireOracleGate(t, fixture, OperationStartRequest, wireOracleActive)
			_, err := NewStartRequestWireRequest(gate, otherRunPolicy, fixture.intent)
			return err
		}},
		{name: "finish wire", call: func() error {
			gate := wireOracleGate(t, fixture, OperationFinishRequest, wireOracleActive)
			_, err := NewFinishRequestWireRequest(gate, otherRunPolicy, fixture.intent)
			return err
		}},
		{name: "cancel wire", call: func() error {
			gate := wireOracleGate(t, fixture, OperationCancelReservation, wireOracleActive)
			_, err := NewCancelReservationWireRequest(gate, otherRunPolicy, fixture.intent)
			return err
		}},
		{name: "request transcript", call: func() error {
			_, err := NewRequestTranscript(otherRunPolicy, fixture.job, document)
			return err
		}},
	}
	for _, consumer := range consumers {
		t.Run(consumer.name, func(t *testing.T) {
			if err := consumer.call(); !errors.Is(err, ErrPolicyGroupBindingMismatch) {
				t.Fatalf("cross-run authority error = %v", err)
			}
		})
	}
}

func TestReservationOrdinalAuthorityBoundary(t *testing.T) {
	if MaxReservationCreationsPerRun != 100 {
		t.Fatalf("MaxReservationCreationsPerRun = %d, want 100", MaxReservationCreationsPerRun)
	}
	fixture := newWireOracleFixture(t)
	intent := fixture.intent
	intent.RequestOrdinal = 100
	if _, err := DeriveReservationID(fixture.runPolicy, intent); err != nil {
		t.Fatalf("ordinal 100 rejected: %v", err)
	}
	gate := wireOracleGate(t, fixture, OperationReserveRequest, wireOracleActive)
	if _, err := NewReserveRequestWireRequest(gate, fixture.runPolicy, intent); err != nil {
		t.Fatalf("ordinal 100 reserve wire rejected: %v", err)
	}

	intent.RequestOrdinal = 101
	if _, err := DeriveReservationID(fixture.runPolicy, intent); !errors.Is(err, ErrInvalidUnsignedDecimal) {
		t.Fatalf("ordinal 101 digest error = %v", err)
	}
	if _, err := NewReserveRequestWireRequest(gate, fixture.runPolicy, intent); !errors.Is(err, ErrInvalidUnsignedDecimal) {
		t.Fatalf("ordinal 101 reserve wire error = %v", err)
	}
}

func mutateRunPolicyTuple(decision PolicyDecision) PolicyDecision {
	if decision.GroupConcurrency < MaxScopeConcurrency {
		decision.GroupConcurrency++
	} else {
		decision.GroupConcurrency--
	}
	decision.OriginConcurrency = decision.GroupConcurrency
	if decision.GroupIntervalMS < MaxScopeIntervalMilliseconds {
		decision.GroupIntervalMS++
	} else {
		decision.GroupIntervalMS--
	}
	decision.OriginIntervalMS = decision.GroupIntervalMS
	return decision
}
