package crawljobsv2

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSharedFramingVectors(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	if got := hex.EncodeToString(U64(1)); got != fixture.Expected.Framing.U641Hex {
		t.Fatalf("U64 vector = %s, want %s", got, fixture.Expected.Framing.U641Hex)
	}
	if got := hex.EncodeToString(F([]byte("A"))); got != fixture.Expected.Framing.FAHex {
		t.Fatalf("F vector = %s, want %s", got, fixture.Expected.Framing.FAHex)
	}
	record, err := EncodeRecord(Record{textField("a", "b"), textField("x", "")})
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(record); got != fixture.Expected.Framing.RecordHex {
		t.Fatalf("RECORD vector = %s, want %s", got, fixture.Expected.Framing.RecordHex)
	}
	encodedSection, err := EncodeSection("s", []Record{{textField("a", "b")}})
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(encodedSection); got != fixture.Expected.Framing.SectionHex {
		t.Fatalf("SECTION vector = %s, want %s", got, fixture.Expected.Framing.SectionHex)
	}
	if _, err := EncodeRecord(Record{textField("a", "1"), textField("a", "2")}); !errors.Is(err, ErrDuplicateFieldName) {
		t.Fatalf("duplicate field error = %v", err)
	}
	if _, err := EncodeSection("é", nil); !errors.Is(err, ErrInvalidSectionLabel) {
		t.Fatalf("section label error = %v", err)
	}
}

func TestSharedIdentityAndPolicyVectors(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	if got := DeriveGlobalScopeID(); string(got) != fixture.Expected.ScopeIDs["global"] {
		t.Fatal("global scope vector mismatch")
	}
	rateScopeID, err := ParseRateScopeID(fixture.Identities.RateScopeID)
	if err != nil {
		t.Fatal(err)
	}
	groupScopeID, err := DeriveGroupScopeID(rateScopeID)
	if err != nil || string(groupScopeID) != fixture.Expected.ScopeIDs["group_a"] {
		t.Fatal("group scope vector mismatch")
	}
	origin, err := DeriveCanonicalOrigin(fixture.Targets["page"].CanonicalURL)
	if err != nil {
		t.Fatal(err)
	}
	originScopeID, err := DeriveOriginScopeID(origin)
	if err != nil || string(originScopeID) != fixture.Expected.ScopeIDs["origin"] {
		t.Fatal("origin scope vector mismatch")
	}
	if len(fixture.Expected.URLIDs) != len(fixture.Targets) {
		t.Fatalf("URL ID vectors = %d, targets = %d", len(fixture.Expected.URLIDs), len(fixture.Targets))
	}
	for name, target := range fixture.Targets {
		expected, ok := fixture.Expected.URLIDs[name]
		if !ok {
			t.Fatalf("missing URL ID vector %q", name)
		}
		identity, err := requireCanonicalURL(target.CanonicalURL)
		if err != nil || identity.URLID != expected || target.URLID != expected {
			t.Fatalf("URL ID %q mismatch: computed=%q target=%q expected=%q err=%v", name, identity.URLID, target.URLID, expected, err)
		}
	}
	for name, expected := range fixture.Expected.TargetDigests {
		digest, err := DeriveTargetDigest(vectorTargetValue(t, fixture, name))
		if err != nil || string(digest) != expected {
			t.Fatalf("target digest %q mismatch: err=%v", name, err)
		}
	}
	for name, expected := range fixture.Expected.PolicyDecisionDigests {
		digest, err := DerivePolicyDecisionDigest(vectorDecisionValue(t, fixture, name))
		if err != nil || string(digest) != expected {
			t.Fatalf("policy decision %q mismatch: err=%v", name, err)
		}
	}

	groups := make([]PolicyGroup, 0, len(fixture.PolicyGroups))
	for _, vector := range fixture.PolicyGroups {
		groupID, err := ParseGroupID(vector.GroupID)
		if err != nil {
			t.Fatal(err)
		}
		rateID, err := ParseRateScopeID(vector.RateScopeID)
		if err != nil {
			t.Fatal(err)
		}
		scopeID, err := DeriveGroupScopeID(rateID)
		if err != nil {
			t.Fatal(err)
		}
		groups = append(groups, PolicyGroup{
			GroupID: groupID, RateScopeID: rateID, GroupScopeID: scopeID,
			RequestStartLimit: vector.RequestStartLimit, Concurrency: vector.Concurrency,
			IntervalMS: vector.IntervalMS,
		})
	}
	groupMapDigest, err := DerivePolicyGroupMapDigest(groups)
	if err != nil || string(groupMapDigest) != fixture.Expected.PolicyGroupMapDigest {
		t.Fatalf("group map vector mismatch: err=%v", err)
	}
	groups[0], groups[1] = groups[1], groups[0]
	reordered, err := DerivePolicyGroupMapDigest(groups)
	if err != nil || reordered != groupMapDigest {
		t.Fatalf("group map input ordering changed digest: err=%v", err)
	}

	lease := vectorLease(t, fixture)
	tokenDigest, err := DeriveTokenDigest(lease)
	if err != nil || string(tokenDigest) != fixture.Expected.TokenDigest {
		t.Fatalf("token digest vector mismatch: err=%v", err)
	}
	alternateOwner, err := ParseOwnerID(fixture.Identities.AlternateOwnerID)
	if err != nil {
		t.Fatal(err)
	}
	lease.OwnerID = alternateOwner
	alternateTokenDigest, err := DeriveTokenDigest(lease)
	if err != nil || alternateTokenDigest != tokenDigest {
		t.Fatalf("owner entered token digest: err=%v", err)
	}
}

func TestSharedTransitionAndReservationVectors(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	lease := vectorLease(t, fixture)
	runPolicy := vectorRunPolicyAuthority(t, fixture, plainSHA256(testDenyAllRenderPolicyArtifact()))
	reservationIntent := vectorReservationValue(t, fixture, false)
	reservationID, err := DeriveReservationID(runPolicy, reservationIntent)
	if err != nil || string(reservationID) != fixture.Expected.ReservationID {
		t.Fatalf("reservation vector mismatch: err=%v", err)
	}
	alternateOwner, err := ParseOwnerID(fixture.Identities.AlternateOwnerID)
	if err != nil {
		t.Fatal(err)
	}
	reservationIntent.Lease.OwnerID = alternateOwner
	alternateReservationID, err := DeriveReservationID(runPolicy, reservationIntent)
	if err != nil || alternateReservationID != reservationID {
		t.Fatalf("owner entered reservation ID: err=%v", err)
	}

	reasons := fixture.TransitionReasons
	commitID, err := ParseDigest(fixture.Expected.CommitID)
	if err != nil {
		t.Fatal(err)
	}
	transitions := map[string]func() (Digest, error){
		"reject_ready": func() (Digest, error) {
			return DeriveRejectReadyTransitionID(runPolicy, RejectReadyTransitionInput{
				RunID: lease.RunID, Job: vectorSourceJobValue(t, fixture, fixture.TryClaim.SourceJobIndex),
				Reason: Reason(reasons["reject_ready"]),
			})
		},
		"try_claim": func() (Digest, error) {
			return DeriveTryClaimTransitionID(runPolicy, TryClaimTransitionInput{
				Job: vectorSourceJobValue(t, fixture, fixture.TryClaim.SourceJobIndex), Lease: lease,
				ExpectedPriorFence: fixture.TryClaim.ExpectedPriorFence,
				InitialIntent:      vectorReservationValue(t, fixture, true),
			})
		},
		"release_before_io": func() (Digest, error) {
			return DeriveReleaseBeforeIOTransitionID(ReleaseBeforeIOTransitionInput{Lease: lease})
		},
		"retry": func() (Digest, error) {
			return DeriveRetryTransitionID(RetryTransitionInput{Lease: lease, Reason: Reason(reasons["retry"])})
		},
		"dead": func() (Digest, error) {
			return DeriveDeadTransitionID(DeadTransitionInput{Lease: lease, Reason: Reason(reasons["dead"])})
		},
		"cancel_job": func() (Digest, error) {
			return DeriveCancelJobTransitionID(CancelJobTransitionInput{Lease: lease, Reason: Reason(reasons["cancel_job"])})
		},
		"complete_no_output": func() (Digest, error) {
			return DeriveCompleteNoOutputTransitionID(CompleteNoOutputTransitionInput{
				Lease: lease, Reason: Reason(reasons["complete_no_output"]),
			})
		},
		"abort_stage": func() (Digest, error) {
			return DeriveAbortStageTransitionID(AbortStageTransitionInput{Lease: lease, CommitID: commitID})
		},
	}
	payloads := baselineTransitionPayloads(t, fixture, runPolicy, commitID)
	if len(payloads) != len(fixture.Expected.TransitionPayloads) {
		t.Fatalf("transition payload vectors = %d, computed = %d", len(fixture.Expected.TransitionPayloads), len(payloads))
	}
	for name, payload := range payloads {
		digest, err := deriveTransitionPayloadDigest(payload)
		if err != nil || string(digest) != fixture.Expected.TransitionPayloads[name] {
			t.Fatalf("transition payload %q vector mismatch: err=%v", name, err)
		}
	}
	if len(transitions) != len(fixture.Expected.TransitionIDs) {
		t.Fatalf("transition ID vectors = %d, computed = %d", len(fixture.Expected.TransitionIDs), len(transitions))
	}
	for name, derive := range transitions {
		digest, err := derive()
		if err != nil || string(digest) != fixture.Expected.TransitionIDs[name] {
			t.Fatalf("transition %q vector mismatch: err=%v", name, err)
		}
	}

	base, err := DeriveRetryTransitionID(RetryTransitionInput{Lease: lease, Reason: ReasonRequestTimeout})
	if err != nil {
		t.Fatal(err)
	}
	lease.OwnerID = alternateOwner
	changedOwner, err := DeriveRetryTransitionID(RetryTransitionInput{Lease: lease, Reason: ReasonRequestTimeout})
	if err != nil || changedOwner == base {
		t.Fatalf("transition payload did not bind owner: err=%v", err)
	}
	changedReason, err := DeriveRetryTransitionID(RetryTransitionInput{Lease: vectorLease(t, fixture), Reason: ReasonDNSTemporary})
	if err != nil || changedReason == base {
		t.Fatalf("transition identity did not bind reason: err=%v", err)
	}
}

func baselineTransitionPayloads(t *testing.T, fixture digestVectorFixture, runPolicy RunPolicyAuthority, commitID Digest) map[string]Record {
	t.Helper()
	job := vectorSourceJobValue(t, fixture, fixture.TryClaim.SourceJobIndex)
	jobRecord, err := completeSourceJobRecord(job)
	if err != nil {
		t.Fatal(err)
	}
	intent := vectorReservationValue(t, fixture, true)
	intentFields, err := reservationIntentFields(runPolicy, intent)
	if err != nil {
		t.Fatal(err)
	}
	lease := vectorLease(t, fixture)
	ownerPayload := Record{textField("owner_id", string(lease.OwnerID))}
	tryClaimPayload := Record{
		cloneField(jobRecord[1]),
		cloneField(jobRecord[2]),
		cloneField(jobRecord[3]),
		textField("job_group_id", string(job.GroupID)),
		textField("job_rate_scope_id", string(job.RateScopeID)),
		textField("job_group_scope_id", string(job.Decision.GroupScopeID)),
		textField("job_initial_origin_scope_id", string(job.Decision.OriginScopeID)),
		textField("job_policy_decision_sha256", string(jobRecord[8].Value)),
		textField("expected_prior_fence", canonicalDecimal(fixture.TryClaim.ExpectedPriorFence)),
		textField("owner_id", string(lease.OwnerID)),
	}
	tryClaimPayload = append(tryClaimPayload, intentFields...)
	return map[string]Record{
		"reject_ready":       cloneRecord(jobRecord[1:]),
		"try_claim":          tryClaimPayload,
		"release_before_io":  cloneRecord(ownerPayload),
		"retry":              cloneRecord(ownerPayload),
		"dead":               cloneRecord(ownerPayload),
		"cancel_job":         cloneRecord(ownerPayload),
		"complete_no_output": cloneRecord(ownerPayload),
		"abort_stage": append(
			cloneRecord(ownerPayload),
			textField("commit_id", string(commitID)),
		),
	}
}

func TestTransitionReasonMatrixIsExhaustivelyClosed(t *testing.T) {
	allowed := map[OperationName][]Reason{
		OperationRejectReady: {
			ReasonPolicyDenied, ReasonPolicyScopeChanged, ReasonJobMalformed,
			ReasonURLIdentityMismatch, ReasonStaticURLDenied,
		},
		OperationTryClaim:        {ReasonNone},
		OperationReleaseBeforeIO: {ReasonNone},
		OperationRetry: {
			ReasonRequestTimeout, ReasonDNSTemporary, ReasonDialTemporary, ReasonRequestTemporary,
			ReasonHTTP429, ReasonHTTP5xx, ReasonRobotsTemporary, ReasonRendererTemporary,
			ReasonDownstreamBackpressure, ReasonCapacityBlockedAfterIO,
			ReasonRunBudgetExhaustedAfterIO, ReasonGroupBudgetExhaustedAfterIO,
			ReasonRateBlockedAfterIO, ReasonWorkerShutdownAfterIO,
		},
		OperationDead: {
			ReasonPolicyDenied, ReasonRobotsDenied, ReasonRobotsInvalid, ReasonJobMalformed,
			ReasonURLIdentityMismatch, ReasonStaticURLDenied, ReasonDNSProhibited, ReasonHTTP4xx,
			ReasonResponseInvalid, ReasonBodyTooLarge, ReasonHTMLInvalid, ReasonDiscoveryLimit,
			ReasonRendererPermanent, ReasonOutputInvalid, ReasonRunJobLimit,
			ReasonReservationLimitExhausted, ReasonProtocolCorrupt,
		},
		OperationCancelJob:        {ReasonAuthorizationExpired, ReasonOperatorCancelled, ReasonSourceCancelled},
		OperationCompleteNoOutput: {ReasonAlreadyVisited},
		OperationAbortStage:       {ReasonNone},
	}
	for operation, accepted := range allowed {
		acceptedSet := make(map[Reason]struct{}, len(accepted))
		for _, reason := range accepted {
			acceptedSet[reason] = struct{}{}
		}
		for reason := range reasons {
			_, wantAllowed := acceptedSet[reason]
			err := ValidateTransitionReason(operation, reason)
			if wantAllowed && err != nil {
				t.Fatalf("%s rejected permitted reason %q: %v", operation, reason, err)
			}
			if !wantAllowed && !errors.Is(err, ErrInvalidTransitionReason) {
				t.Fatalf("%s accepted or misclassified forbidden reason %q: %v", operation, reason, err)
			}
		}
	}
	if err := ValidateTransitionReason(OperationCommit, ReasonNone); !errors.Is(err, ErrUnknownOperation) {
		t.Fatalf("non-transition operation error = %v", err)
	}
}

func TestSharedSourceOutputPublicationCommitAndChunkVectors(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	sourceJobs := make([]SourceJob, 0, len(fixture.SourceJobs))
	for index := range fixture.SourceJobs {
		sourceJobs = append(sourceJobs, vectorSourceJobValue(t, fixture, index))
	}
	sourceDigest, err := DeriveSourceDigest(sourceJobs)
	if err != nil || string(sourceDigest) != fixture.Expected.SourceDigest {
		t.Fatalf("source vector mismatch: err=%v", err)
	}
	sourceJobs[0], sourceJobs[1] = sourceJobs[1], sourceJobs[0]
	reorderedSource, err := DeriveSourceDigest(sourceJobs)
	if err != nil || reorderedSource != sourceDigest {
		t.Fatalf("source input ordering changed digest: err=%v", err)
	}

	context := vectorOutputContextValue(t, fixture)
	output := vectorOutputValue(t, fixture)
	outputDigest, err := DeriveOutputDigest(context, output)
	if err != nil || string(outputDigest) != fixture.Expected.OutputDigest {
		t.Fatalf("output vector mismatch: err=%v", err)
	}
	reorderedOutput := output
	reorderedOutput.Outlinks = []string{output.Outlinks[1], output.Outlinks[0]}
	reorderedOutput.Images = []OutputImage{output.Images[1], output.Images[0]}
	reorderedOutput.Discoveries = []OutputDiscovery{output.Discoveries[1], output.Discoveries[0]}
	reorderedDigest, err := DeriveOutputDigest(context, reorderedOutput)
	if err != nil || reorderedDigest != outputDigest {
		t.Fatalf("output input ordering changed digest: err=%v", err)
	}

	lease := vectorLease(t, fixture)
	publicationID, err := DerivePublicationID(PublicationIdentity{
		RunID: lease.RunID, JobID: lease.JobID, Fence: lease.Fence, OutputDigest: outputDigest,
	})
	if err != nil || string(publicationID) != fixture.Expected.PublicationID {
		t.Fatalf("publication vector mismatch: err=%v", err)
	}
	identity := CommitIdentity{
		RunID: lease.RunID, JobID: lease.JobID, OwnerID: lease.OwnerID,
		Fence: lease.Fence, Token: lease.Token, PublicationID: publicationID,
		RequestStartsBaseline: context.requestStartsBaseline, RequestStartsGeneration: context.requestStartsGeneration,
	}
	commitID, err := DeriveCommitID(identity)
	if err != nil || string(commitID) != fixture.Expected.CommitID {
		t.Fatalf("commit vector mismatch: err=%v", err)
	}
	chunkURLs := make([]string, 0, len(fixture.Chunk.Records))
	for _, vector := range fixture.Chunk.Records {
		chunkURLs = append(chunkURLs, vector.TargetURL)
	}
	chunk, err := NewOutlinksStageChunk(identity, fixture.Chunk.Ordinal, context, chunkURLs)
	if err != nil {
		t.Fatal(err)
	}
	chunkDigest, err := DeriveChunkDigest(chunk)
	if err != nil || string(chunkDigest) != fixture.Expected.ChunkDigest {
		t.Fatalf("chunk vector mismatch: err=%v", err)
	}
	records := chunk.Records()
	records[0][0].Value[0] = 'X'
	redigested, err := DeriveChunkDigest(chunk)
	if err != nil || redigested != chunkDigest {
		t.Fatalf("chunk accessor exposed mutable backing data: err=%v", err)
	}
}

// TestDigestDerivationBoundariesRejectZeroSHA256 exercises each derivation
// family that accepts an already-derived digest, scope, or reservation lineage
// from its caller. Derived outputs are not allowed to turn the bootstrap
// sentinel into a normal production identity.
func TestDigestDerivationBoundariesRejectZeroSHA256(t *testing.T) {
	fixture := newWireOracleFixture(t)
	vectorFixture := loadDigestVectorFixture(t)
	outputContext := vectorOutputContextValue(t, vectorFixture)
	output := vectorOutputValue(t, vectorFixture)
	if len(output.Discoveries) == 0 {
		t.Fatal("digest vector has no discovery for zero derivation coverage")
	}
	zero := Digest(ZeroSHA256)

	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "policy decision target digest",
			call: func() error {
				decision := fixture.job.Decision
				decision.TargetDigest = zero
				_, err := DerivePolicyDecisionDigest(decision)
				return err
			},
		},
		{
			name: "policy decision global scope",
			call: func() error {
				decision := fixture.job.Decision
				decision.GlobalScopeID = zero
				_, err := DerivePolicyDecisionDigest(decision)
				return err
			},
		},
		{
			name: "policy decision group scope",
			call: func() error {
				decision := fixture.job.Decision
				decision.GroupScopeID = zero
				_, err := DerivePolicyDecisionDigest(decision)
				return err
			},
		},
		{
			name: "policy decision origin scope",
			call: func() error {
				decision := fixture.job.Decision
				decision.OriginScopeID = zero
				_, err := DerivePolicyDecisionDigest(decision)
				return err
			},
		},
		{
			name: "policy group scope",
			call: func() error {
				group := fixture.policyGroup
				group.GroupScopeID = zero
				_, err := DerivePolicyGroupMapDigest([]PolicyGroup{group})
				return err
			},
		},
		{
			name: "reservation crawl policy",
			call: func() error {
				intent := fixture.intent
				intent.CrawlPolicyDigest = zero
				_, err := DeriveReservationID(fixture.runPolicy, intent)
				return err
			},
		},
		{
			name: "reservation decision scope",
			call: func() error {
				intent := fixture.intent
				intent.Decision.OriginScopeID = zero
				_, err := DeriveReservationID(fixture.runPolicy, intent)
				return err
			},
		},
		{
			name: "publication output digest",
			call: func() error {
				_, err := DerivePublicationID(PublicationIdentity{
					RunID: fixture.runID, JobID: fixture.job.JobID, Fence: fixture.lease.Fence, OutputDigest: zero,
				})
				return err
			},
		},
		{
			name: "commit publication ID",
			call: func() error {
				identity := fixture.commitIdentity
				identity.PublicationID = zero
				_, err := DeriveCommitID(identity)
				return err
			},
		},
		{
			name: "source policy digest lineage",
			call: func() error {
				job := fixture.job
				job.Decision.TargetDigest = zero
				_, err := DeriveSourceDigest([]SourceJob{job})
				return err
			},
		},
		{
			name: "output optional render-policy digest",
			call: func() error {
				candidate := output
				candidate.Page.RenderPolicyDigest = zero
				_, err := DeriveOutputDigest(outputContext, candidate)
				return err
			},
		},
		{
			name: "output discovery policy digest lineage",
			call: func() error {
				candidate := output
				candidate.Discoveries = append([]OutputDiscovery(nil), output.Discoveries...)
				candidate.Discoveries[0].Decision.GroupScopeID = zero
				_, err := DeriveOutputDigest(outputContext, candidate)
				return err
			},
		},
		{
			name: "chunk commit ID",
			call: func() error {
				chunk := fixture.chunks[OperationStageOutlinksBatch]
				chunk.commitID = zero
				_, err := DeriveChunkDigest(chunk)
				return err
			},
		},
		{
			name: "reject transition source digest lineage",
			call: func() error {
				job := fixture.job
				job.Decision.GroupScopeID = zero
				_, err := DeriveRejectReadyTransitionID(fixture.runPolicy, RejectReadyTransitionInput{
					RunID: fixture.runID, Job: job, Reason: ReasonPolicyDenied,
				})
				return err
			},
		},
		{
			name: "claim transition reservation lineage",
			call: func() error {
				intent := fixture.intent
				intent.CrawlPolicyDigest = zero
				_, err := DeriveTryClaimTransitionID(fixture.runPolicy, TryClaimTransitionInput{
					Job: fixture.job, Lease: fixture.lease, ExpectedPriorFence: 1, InitialIntent: intent,
				})
				return err
			},
		},
		{
			name: "abort transition commit ID",
			call: func() error {
				_, err := DeriveAbortStageTransitionID(AbortStageTransitionInput{Lease: fixture.lease, CommitID: zero})
				return err
			},
		},
		{
			name: "operational scope reference",
			call: func() error {
				_, err := DeriveOperationalReference(make([]byte, MinOperationalReferenceKeyBytes), OperationalReferenceScope, ZeroSHA256)
				return err
			},
		},
	}
	if len(tests) != 17 {
		t.Fatalf("zero derivation manifest = %d, want 17", len(tests))
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.call(); err == nil {
				t.Fatal("derivation accepted ZERO_SHA256")
			}
		})
	}
}

func TestAuthoritativeOutputContextDerivesTimestampAndAliases(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	context := vectorOutputContextValue(t, fixture)
	pageRecord, err := outputPageRecord(context, vectorOutputValue(t, fixture).Page)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(pageRecord[5].Value); got != "Tue, 01 Sep 2026 12:34:56 UTC" {
		t.Fatalf("last_crawled = %q", got)
	}
	aliases, err := outputAliasRecords(context.aliases)
	if err != nil {
		t.Fatal(err)
	}
	if len(aliases) != 2 {
		t.Fatalf("alias count = %d, want 2", len(aliases))
	}
	if string(aliases[0][0].Value) != fixture.Targets["target"].URLID || string(aliases[1][0].Value) != fixture.Targets["page"].URLID {
		t.Fatal("aliases were not derived and sorted from the authoritative request chain")
	}
	output := vectorOutputValue(t, fixture)
	output.Page.NormalizedURL = fixture.Targets["page"].CanonicalURL
	if _, err := DeriveOutputDigest(context, output); !errors.Is(err, ErrDigestInputMismatch) {
		t.Fatalf("unrecorded effective URL error = %v", err)
	}
}

func TestSuccessfulDocumentEvidenceBindsReservationAndStartResponse(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	intent := vectorReservationValue(t, fixture, true)
	artifact := testDenyAllRenderPolicyArtifact()
	runPolicy := newAuthenticatedTestRunPolicyAuthority(
		t, intent.Lease.RunID, intent.CrawlPolicyDigest, plainSHA256(artifact), vectorPolicyGroupsValue(t, fixture),
	)
	reservationID, err := DeriveReservationID(runPolicy, intent)
	if err != nil {
		t.Fatal(err)
	}
	startedAt := RedisMilliseconds(fixture.OutputContext.Requests[0].StartedAtMS)
	raw := []string{
		string(StatusStarted), canonicalDecimal(uint64(startedAt)), string(reservationID),
		canonicalDecimal(uint64(startedAt)), "1", "1", "1", "1", "1",
	}
	response, err := newTestTransportAuthority().parseStartRequestResponse(runPolicy, intent, raw)
	if err != nil {
		t.Fatalf("valid response: %v", err)
	}
	permit, err := response.IOPermit()
	if err != nil {
		t.Fatalf("valid permit: %v", err)
	}
	evidence, err := NewSuccessfulDocumentRequest(permit)
	if err != nil {
		t.Fatalf("valid evidence: %v", err)
	}
	source := vectorSourceJobValue(t, fixture, 0)
	transcript, err := NewDocumentTranscript(runPolicy, source, evidence)
	if err != nil {
		t.Fatal(err)
	}
	targetDigest, err := DeriveTargetDigest(intent.Target)
	if err != nil {
		t.Fatal(err)
	}
	witness, err := newTestTransportAuthority().parseFinalDocumentWitness(intent.Lease, []string{
		canonicalDecimal(uint64(startedAt)), canonicalDecimal(uint64(intent.Lease.Fence)),
		string(intent.Target.URLID), intent.Target.CanonicalURL, string(targetDigest),
		"1", canonicalDecimal(uint64(startedAt)),
		"0", "leased", string(intent.Lease.OwnerID), string(intent.Lease.Token), canonicalDecimal(uint64(intent.Lease.Fence)), "",
	})
	if err != nil {
		t.Fatal(err)
	}
	renderPolicy, err := NewRenderPolicyAuthorization(runPolicy, artifact)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewOutputContext(runPolicy, source, transcript, witness, renderPolicy); err != nil {
		t.Fatalf("valid evidence chain: %v", err)
	}
	otherLease := intent.Lease
	otherLease.OwnerID, err = ParseOwnerID(fixture.Identities.AlternateOwnerID)
	if err != nil {
		t.Fatal(err)
	}
	otherWitness, err := newTestTransportAuthority().parseFinalDocumentWitness(otherLease, []string{
		canonicalDecimal(uint64(startedAt)), canonicalDecimal(uint64(otherLease.Fence)),
		string(intent.Target.URLID), intent.Target.CanonicalURL, string(targetDigest),
		"1", canonicalDecimal(uint64(startedAt)),
		"0", "leased", string(otherLease.OwnerID), string(otherLease.Token), canonicalDecimal(uint64(otherLease.Fence)), "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewOutputContext(runPolicy, source, transcript, otherWitness, renderPolicy); !errors.Is(err, ErrDigestInputMismatch) {
		t.Fatalf("cross-owner evidence error = %v", err)
	}

	raw[2] = fixture.Expected.ReservationID
	if _, err := newTestTransportAuthority().parseStartRequestResponse(runPolicy, intent, raw); !errors.Is(err, ErrResponseScalar) {
		t.Fatalf("mismatched reservation evidence error = %v", err)
	}
	if _, err := NewOutputContext(runPolicy, source, DocumentTranscript{}, witness, renderPolicy); !errors.Is(err, ErrDigestInputMismatch) {
		t.Fatalf("uninitialized request evidence error = %v", err)
	}
}

func TestStartRequestAuthorityIsOneUseAcrossResponseAndPermitCopies(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	intent := vectorReservationValue(t, fixture, true)
	runPolicy := vectorRunPolicyAuthority(t, fixture, plainSHA256(testDenyAllRenderPolicyArtifact()))
	reservationID, err := DeriveReservationID(runPolicy, intent)
	if err != nil {
		t.Fatal(err)
	}
	startedAt := fixture.OutputContext.Requests[0].StartedAtMS
	response, err := newTestTransportAuthority().parseStartRequestResponse(runPolicy, intent, []string{
		string(StatusStarted), canonicalDecimal(startedAt), string(reservationID), canonicalDecimal(startedAt),
		"1", "1", "1", "1", "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	responseCopy := response
	permit, err := response.IOPermit()
	if err != nil {
		t.Fatalf("first permit: %v", err)
	}
	if _, err := responseCopy.IOPermit(); !errors.Is(err, ErrInvalidResponseAuthority) {
		t.Fatalf("copied response issued a second permit: %v", err)
	}
	permitCopy := permit
	evidence, err := NewSuccessfulRequest(permit)
	if err != nil {
		t.Fatalf("first permit consumption: %v", err)
	}
	if evidence.target != intent.Target || evidence.lease != intent.Lease || evidence.requestOrdinal != intent.RequestOrdinal {
		t.Fatal("successful evidence lost its target, lease, or intent binding")
	}
	if _, err := NewSuccessfulRequest(permitCopy); !errors.Is(err, ErrInvalidResponseAuthority) {
		t.Fatalf("copied permit was consumed twice: %v", err)
	}

	tampered := evidence
	tampered.target = mustFixtureTargetForURL(t, "https://authority-tamper.example.org/document")
	if err := validateSuccessfulDocumentRequest(tampered); !errors.Is(err, ErrInvalidResponseAuthority) {
		t.Fatalf("tampered evidence error = %v", err)
	}
}

func TestDocumentTranscriptRepresentsNonAliasStartsAndRejectsGapsReorderingAndLeaseLoss(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	source := vectorSourceJobValue(t, fixture, fixture.OutputContext.SourceJobIndex)
	lease := vectorLease(t, fixture)
	policyDigest := mustFixtureDigest(t, fixture.Identities.CrawlPolicyDigest)
	artifact := testDenyAllRenderPolicyArtifact()
	runPolicy := newAuthenticatedTestRunPolicyAuthority(
		t, lease.RunID, policyDigest, plainSHA256(artifact), vectorPolicyGroupsValue(t, fixture),
	)
	robotsTarget := mustFixtureTargetForURL(t, "https://robots-flow.example.org/robots.txt")
	redirectTarget := mustFixtureTargetForURL(t, "https://redirect-flow.example.org/final")
	resourceTarget := mustFixtureTargetForURL(t, "https://resource-flow.example.org/app.js")

	robots := newTestSuccessfulStartEvent(t, runPolicy, source, lease, policyDigest, RequestRobots, robotsTarget, 1, 1_788_266_090_000, 1, 1, 1)
	document := newTestSuccessfulStartEvent(t, runPolicy, source, lease, policyDigest, RequestDocument,
		RequestTarget{URLID: source.JobID, CanonicalURL: source.CanonicalURL}, 3, 1_788_266_091_000, 2, 2, 2)
	redirect := newTestSuccessfulStartEvent(t, runPolicy, source, lease, policyDigest, RequestRedirect, redirectTarget, 4, 1_788_266_092_000, 3, 3, 3)
	resource := newTestSuccessfulStartEvent(t, runPolicy, source, lease, policyDigest, RequestRenderResource, resourceTarget, 6, 1_788_266_093_000, 4, 4, 4)

	transcript, err := NewRequestTranscript(runPolicy, source, robots)
	if err != nil {
		t.Fatalf("robots-first transcript: %v", err)
	}
	for _, event := range []SuccessfulDocumentRequest{document, redirect, resource} {
		transcript, err = transcript.AppendSuccessfulRequest(event)
		if err != nil {
			t.Fatalf("append %s event: %v", event.kind, err)
		}
	}
	aliases, err := transcriptAliases(transcript)
	if err != nil {
		t.Fatal(err)
	}
	if len(aliases) != 2 {
		t.Fatalf("aliases include robots/render resources: got %d, want 2", len(aliases))
	}
	for _, alias := range aliases {
		if alias.URLID == robotsTarget.URLID || alias.URLID == resourceTarget.URLID {
			t.Fatal("non-document event produced an alias")
		}
	}
	redirectDigest, err := DeriveTargetDigest(redirectTarget)
	if err != nil {
		t.Fatal(err)
	}
	witness, err := newTestTransportAuthority().parseFinalDocumentWitness(lease, []string{
		canonicalDecimal(uint64(redirect.redisStartedAtMS)), canonicalDecimal(uint64(lease.Fence)),
		string(redirectTarget.URLID), redirectTarget.CanonicalURL, string(redirectDigest),
		canonicalDecimal(resource.jobRequestStarts), canonicalDecimal(uint64(resource.redisStartedAtMS)),
		"0", "leased", string(lease.OwnerID), string(lease.Token), canonicalDecimal(uint64(lease.Fence)), "",
	})
	if err != nil {
		t.Fatal(err)
	}
	renderPolicy, err := NewRenderPolicyAuthorization(runPolicy, artifact)
	if err != nil {
		t.Fatal(err)
	}
	context, err := NewOutputContext(runPolicy, source, transcript, witness, renderPolicy)
	if err != nil {
		t.Fatalf("resource-after-final-document context: %v", err)
	}
	if context.finalTarget != redirectTarget || len(context.aliases) != 2 {
		t.Fatal("output context did not use the final alias-bearing request")
	}

	firstDocument := newTestSuccessfulStartEvent(t, runPolicy, source, lease, policyDigest, RequestDocument,
		RequestTarget{URLID: source.JobID, CanonicalURL: source.CanonicalURL}, 10, 1_788_266_094_000, 1, 1, 1)
	omittedRedirect := newTestSuccessfulStartEvent(t, runPolicy, source, lease, policyDigest, RequestRedirect,
		mustFixtureTargetForURL(t, "https://redirect-flow.example.org/omitted"), 11, 1_788_266_095_000, 2, 2, 2)
	laterRedirect := newTestSuccessfulStartEvent(t, runPolicy, source, lease, policyDigest, RequestRedirect,
		mustFixtureTargetForURL(t, "https://redirect-flow.example.org/later"), 12, 1_788_266_096_000, 3, 3, 3)
	shortTranscript, err := NewDocumentTranscript(runPolicy, source, firstDocument)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := shortTranscript.AppendSuccessfulRequest(laterRedirect); !errors.Is(err, ErrDigestInputMismatch) {
		t.Fatalf("omitted alias-bearing hop error = %v", err)
	}
	withRedirect, err := shortTranscript.AppendSuccessfulRequest(omittedRedirect)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := withRedirect.AppendSuccessfulRequest(omittedRedirect); !errors.Is(err, ErrDigestInputMismatch) {
		t.Fatalf("replayed event error = %v", err)
	}
	reordered := withRedirect
	reordered.requests = []SuccessfulDocumentRequest{firstDocument, laterRedirect, omittedRedirect}
	if err := validateDocumentTranscript(reordered); !errors.Is(err, ErrDigestInputMismatch) {
		t.Fatalf("reordered transcript error = %v", err)
	}

	otherLease := lease
	otherLease.OwnerID = mustOwnerID(t, fixture.Identities.AlternateOwnerID)
	otherLeaseEvent := newTestSuccessfulStartEvent(t, runPolicy, source, otherLease, policyDigest, RequestRedirect,
		mustFixtureTargetForURL(t, "https://redirect-flow.example.org/other-lease"), 13, 1_788_266_097_000, 3, 3, 3)
	if _, err := withRedirect.AppendSuccessfulRequest(otherLeaseEvent); !errors.Is(err, ErrDigestInputMismatch) {
		t.Fatalf("cross-lease transcript error = %v", err)
	}
	leaseLost, err := newTestTransportAuthority().parseStartRequestResponse(
		runPolicy,
		ReservationIntent{
			Lease: lease, RequestOrdinal: 14, Target: resourceTarget, CrawlPolicyDigest: policyDigest,
			Decision: mustFixtureDecision(t, RequestRenderResource, resourceTarget, source.Depth, source.GroupID, source.RateScopeID,
				source.Decision.GroupConcurrency, source.Decision.GroupIntervalMS),
		},
		[]string{string(StatusLeaseLost), "1788266098000", canonicalDecimal(uint64(lease.Fence + 1))},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := leaseLost.IOPermit(); !errors.Is(err, ErrInvalidResponseAuthority) {
		t.Fatalf("lease loss produced I/O authority: %v", err)
	}
}

func TestRenderPolicyAuthorizationComesOnlyFromExactStrictArtifact(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	lease := vectorLease(t, fixture)
	groups := vectorPolicyGroupsValue(t, fixture)
	crawlPolicySHA256 := mustFixtureDigest(t, fixture.Identities.CrawlPolicyDigest)
	enabledArtifact := testRenderPolicyArtifact("render-main", true)
	enabledDigest := plainSHA256(enabledArtifact)
	runPolicy := newAuthenticatedTestRunPolicyAuthority(t, lease.RunID, crawlPolicySHA256, enabledDigest, groups)
	authorization, err := NewRenderPolicyAuthorization(runPolicy, enabledArtifact)
	if err != nil {
		t.Fatalf("enabled policy: %v", err)
	}
	if _, err := NewRenderPolicyAuthorization(RunPolicyAuthority{}, enabledArtifact); !errors.Is(err, ErrInvalidResponseAuthority) {
		t.Fatalf("zero run authority error = %v", err)
	}
	enabledArtifact[0] = 'X'
	matched, err := authorization.match("https://render.example.org/app")
	if err != nil || !matched.Enabled || matched.ID != "render-main" {
		t.Fatalf("decoded matcher retained caller-owned artifact bytes: rule=%q err=%v", matched.ID, err)
	}
	authorizationCopy := authorization
	if _, err := authorizationCopy.match("https://render.example.org/app"); err != nil {
		t.Fatalf("immutable authorization copy: %v", err)
	}
	tamperedAuthorization := authorization
	tamperedAuthorization.digest = Digest(strings.Repeat("f", 64))
	if err := validateRenderPolicyAuthorization(tamperedAuthorization); !errors.Is(err, ErrInvalidRenderPolicyArtifact) {
		t.Fatalf("tampered authorization error = %v", err)
	}

	mismatchedArtifact := testRenderPolicyArtifact("different-rule", true)
	if _, err := NewRenderPolicyAuthorization(runPolicy, mismatchedArtifact); !errors.Is(err, ErrDigestInputMismatch) {
		t.Fatalf("mismatched policy bytes error = %v", err)
	}

	duplicateArtifact := []byte(strings.Replace(
		string(testRenderPolicyArtifact("DUPLICATE_POLICY_CANARY", true)),
		`"schema_version":1`, `"schema_version":1,"schema_version":1`, 1,
	))
	duplicateRunPolicy := newAuthenticatedTestRunPolicyAuthority(
		t, lease.RunID, crawlPolicySHA256, plainSHA256(duplicateArtifact), groups,
	)
	if _, err := NewRenderPolicyAuthorization(duplicateRunPolicy, duplicateArtifact); !errors.Is(err, ErrInvalidRenderPolicyArtifact) {
		t.Fatalf("duplicate JSON policy error = %v", err)
	} else if strings.Contains(err.Error(), "DUPLICATE_POLICY_CANARY") {
		t.Fatalf("render-policy decoder error disclosed artifact contents: %v", err)
	}

	target := mustFixtureTargetForURL(t, "https://render.example.org/app")
	context := newTestOutputContextForTarget(t, runPolicy, authorization, target, groups[0], crawlPolicySHA256)
	output := CrawlOutput{Page: OutputPage{
		NormalizedURL: target.CanonicalURL, HTML: []byte("<html>rendered</html>"),
		OriginalHTML: []byte("<html>source</html>"), ContentType: "text/html", StatusCode: 200,
		Rendered: true, RenderPolicyDigest: enabledDigest, RenderPolicyRule: "fabricated-rule",
	}}
	if _, err := DeriveOutputDigest(context, output); !errors.Is(err, ErrInvalidOutput) {
		t.Fatalf("fabricated render rule error = %v", err)
	}
	output.Page.RenderPolicyRule = "render-main"
	if _, err := DeriveOutputDigest(context, output); err != nil {
		t.Fatalf("enabled decoded rule was rejected: %v", err)
	}

	disabledArtifact := testRenderPolicyArtifact("render-main", false)
	disabledDigest := plainSHA256(disabledArtifact)
	disabledRunPolicy := newAuthenticatedTestRunPolicyAuthority(t, lease.RunID, crawlPolicySHA256, disabledDigest, groups)
	disabledAuthorization, err := NewRenderPolicyAuthorization(disabledRunPolicy, disabledArtifact)
	if err != nil {
		t.Fatalf("disabled policy: %v", err)
	}
	context = newTestOutputContextForTarget(t, disabledRunPolicy, disabledAuthorization, target, groups[0], crawlPolicySHA256)
	output.Page.RenderPolicyDigest = disabledDigest
	if _, err := DeriveOutputDigest(context, output); !errors.Is(err, ErrInvalidOutput) {
		t.Fatalf("disabled policy authorized rendering: %v", err)
	}
}

func TestRenderedFinalPageRequiresExactDecodedPolicyURLMatch(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	lease := vectorLease(t, fixture)
	groups := vectorPolicyGroupsValue(t, fixture)
	crawlPolicySHA256 := mustFixtureDigest(t, fixture.Identities.CrawlPolicyDigest)
	artifact := testScopedRenderPolicyArtifact(
		t,
		"render-main",
		true,
		"render.example.org",
		nil,
		[]string{"/app/"},
		[]string{"/app/private"},
	)
	renderPolicySHA256 := plainSHA256(artifact)
	runPolicy := newAuthenticatedTestRunPolicyAuthority(t, lease.RunID, crawlPolicySHA256, renderPolicySHA256, groups)
	renderPolicy, err := NewRenderPolicyAuthorization(runPolicy, artifact)
	if err != nil {
		t.Fatal(err)
	}

	matchingTarget := mustFixtureTargetForURL(t, "https://render.example.org/app/ok")
	matchingContext := newTestOutputContextForTarget(
		t, runPolicy, renderPolicy, matchingTarget, groups[0], crawlPolicySHA256,
	)
	validPage := OutputPage{
		NormalizedURL: matchingTarget.CanonicalURL, HTML: []byte("<html>rendered</html>"),
		OriginalHTML: []byte("<html>source</html>"), ContentType: "text/html", StatusCode: 200,
		Rendered: true, RenderPolicyRule: "render-main", RenderPolicyDigest: renderPolicySHA256,
	}
	publicationID := Digest(strings.Repeat("d", 64))
	finalPage, err := NewFinalPageRecord(matchingContext, validPage, publicationID)
	if err != nil {
		t.Fatalf("matching rendered page: %v", err)
	}
	if err := finalPage.ValidateAgainstContext(matchingContext); err != nil {
		t.Fatalf("matching final page authority: %v", err)
	}

	tests := []struct {
		name   string
		target RequestTarget
		ruleID string
	}{
		{name: "wrong host", target: mustFixtureTargetForURL(t, "https://other.example.org/app/ok"), ruleID: "render-main"},
		{name: "wrong path", target: mustFixtureTargetForURL(t, "https://render.example.org/outside"), ruleID: "render-main"},
		{name: "deny prefix", target: mustFixtureTargetForURL(t, "https://render.example.org/app/private/secret"), ruleID: "render-main"},
		{name: "different rule", target: matchingTarget, ruleID: "different-rule"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context := matchingContext
			if test.target != matchingTarget {
				context = newTestOutputContextForTarget(
					t, runPolicy, renderPolicy, test.target, groups[0], crawlPolicySHA256,
				)
			}
			page := validPage
			page.NormalizedURL = test.target.CanonicalURL
			page.RenderPolicyRule = test.ruleID
			if _, err := DeriveOutputDigest(context, CrawlOutput{Page: page}); !errors.Is(err, ErrOutputInvalidRenderRelation) {
				t.Fatalf("rendered output error = %v", err)
			}

			decoded := testFinalPageRecordForAuthority(t, context, page, publicationID)
			if err := decoded.ValidateAgainstContext(context); !errors.Is(err, ErrArtifactMismatch) {
				t.Fatalf("decoded final-page authority error = %v", err)
			}
		})
	}
}

func testFinalPageRecordForAuthority(t *testing.T, context OutputContext, page OutputPage, publicationID Digest) FinalPageRecord {
	t.Helper()
	fields := Record{
		textField("normalized_url", page.NormalizedURL),
		{Name: "html", Value: append([]byte(nil), page.HTML...)},
		{Name: "original_html", Value: append([]byte(nil), page.OriginalHTML...)},
		textField("content_type", page.ContentType),
		textField("status_code", fmt.Sprintf("%03d", page.StatusCode)),
		textField("last_crawled", context.lastCrawled),
		textField("rendered", "true"),
		textField("render_policy_rule", page.RenderPolicyRule),
		textField("render_policy_sha256", string(page.RenderPolicyDigest)),
		textField("publication_id", string(publicationID)),
	}
	record, err := newFinalPageRecord(fields)
	if err != nil {
		t.Fatalf("decode structurally valid final page: %v", err)
	}
	return record
}

func newTestOutputContextForTarget(
	t *testing.T,
	runPolicy RunPolicyAuthority,
	renderPolicy RenderPolicyAuthorization,
	target RequestTarget,
	group PolicyGroup,
	crawlPolicySHA256 Digest,
) OutputContext {
	t.Helper()
	decision, err := NewPolicyDecision(PolicyDecisionInput{
		RequestKind: RequestDocument, Target: target, Depth: 0, GroupID: group.GroupID, RateScopeID: group.RateScopeID,
		GroupConcurrency: group.Concurrency, GroupIntervalMS: group.IntervalMS,
		OriginConcurrency: group.Concurrency, OriginIntervalMS: group.IntervalMS,
	})
	if err != nil {
		t.Fatal(err)
	}
	score, err := ParseScoreText("1")
	if err != nil {
		t.Fatal(err)
	}
	source := SourceJob{
		JobID: target.URLID, CanonicalURL: target.CanonicalURL, ScoreText: score,
		Depth: 0, GroupID: group.GroupID, RateScopeID: group.RateScopeID, Decision: decision,
	}
	binding, err := runPolicy.authenticatedBinding()
	if err != nil {
		t.Fatal(err)
	}
	lease := vectorLease(t, loadDigestVectorFixture(t))
	lease.RunID = binding.runID
	lease.JobID = target.URLID
	request := newTestSuccessfulStartEvent(
		t, runPolicy, source, lease, crawlPolicySHA256, RequestDocument, target, 1, 1_788_266_095_000, 1, 1, 1,
	)
	transcript, err := NewDocumentTranscript(runPolicy, source, request)
	if err != nil {
		t.Fatal(err)
	}
	targetDigest, err := DeriveTargetDigest(target)
	if err != nil {
		t.Fatal(err)
	}
	witness, err := newTestTransportAuthority().parseFinalDocumentWitness(lease, []string{
		canonicalDecimal(uint64(request.redisStartedAtMS)), canonicalDecimal(uint64(lease.Fence)),
		string(target.URLID), target.CanonicalURL, string(targetDigest), "1",
		canonicalDecimal(uint64(request.redisStartedAtMS)),
		"0", "leased", string(lease.OwnerID), string(lease.Token), canonicalDecimal(uint64(lease.Fence)), "",
	})
	if err != nil {
		t.Fatal(err)
	}
	context, err := NewOutputContext(runPolicy, source, transcript, witness, renderPolicy)
	if err != nil {
		t.Fatal(err)
	}
	return context
}

func newTestSuccessfulStartEvent(
	t testing.TB,
	runPolicy RunPolicyAuthority,
	source SourceJob,
	lease LeaseIdentity,
	policyDigest Digest,
	kind RequestKind,
	target RequestTarget,
	ordinal uint64,
	startedAt uint64,
	jobStarts uint64,
	runStarts uint64,
	groupStarts uint64,
) SuccessfulDocumentRequest {
	t.Helper()
	return newTestSuccessfulStartEventWithAttempts(t, runPolicy, source, lease, policyDigest, kind, target, ordinal, startedAt, jobStarts, runStarts, groupStarts, 1)
}

func newTestSuccessfulStartEventWithAttempts(
	t testing.TB, runPolicy RunPolicyAuthority, source SourceJob, lease LeaseIdentity, policyDigest Digest,
	kind RequestKind, target RequestTarget, ordinal, startedAt, jobStarts, runStarts, groupStarts, attempts uint64,
) SuccessfulDocumentRequest {
	t.Helper()
	decision := mustFixtureDecision(
		t, kind, target, source.Depth, source.GroupID, source.RateScopeID,
		source.Decision.GroupConcurrency, source.Decision.GroupIntervalMS,
	)
	intent := ReservationIntent{
		Lease: lease, RequestOrdinal: ordinal, Target: target,
		CrawlPolicyDigest: policyDigest, Decision: decision,
	}
	reservationID, err := DeriveReservationID(runPolicy, intent)
	if err != nil {
		t.Fatal(err)
	}
	response, err := newTestTransportAuthority().parseStartRequestResponse(runPolicy, intent, []string{
		string(StatusStarted), canonicalDecimal(startedAt), string(reservationID), canonicalDecimal(startedAt),
		canonicalDecimal(attempts), canonicalDecimal(jobStarts), canonicalDecimal(runStarts), canonicalDecimal(groupStarts), "1",
	})
	if err != nil {
		t.Fatalf("parse test start event: %v", err)
	}
	permit, err := response.IOPermit()
	if err != nil {
		t.Fatal(err)
	}
	event, err := NewSuccessfulRequest(permit)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func testRenderPolicyArtifact(ruleID string, enabled bool) []byte {
	document := `{"schema_version":1,"default_action":"deny","rules":[{"id":"RULE_ID","enabled":ENABLED,"host_rule":{"host":"render.example.org","match":"exact"},"allow_paths":["/app"],"allow_path_prefixes":[],"deny_path_prefixes":[],"mode":"inline_only","failure_action":"reject_page","resource_rules":[],"network_controls":{"allowed_methods":["GET"],"robots_for_resources":true,"allow_cookies":false,"allow_service_workers":false,"allow_websockets":false,"allow_webrtc":false,"allow_downloads":false,"allow_popups":false,"allow_secondary_documents":false,"allow_javascript_navigation":false},"limits":{"max_render_time_ms":1000,"settle_time_ms":0,"max_resource_requests":0,"max_aggregate_resource_bytes":0,"max_resource_body_bytes":0,"max_rendered_dom_bytes":1048576,"max_dom_nodes":1000,"max_redirect_hops":0,"max_console_bytes":0}}]}`
	document = strings.ReplaceAll(document, "RULE_ID", ruleID)
	document = strings.ReplaceAll(document, "ENABLED", map[bool]string{true: "true", false: "false"}[enabled])
	return []byte(document)
}

func TestDigestDomainsRemainAcyclicAndDistinct(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	digests := []string{
		fixture.Expected.ScopeIDs["global"], fixture.Expected.ScopeIDs["group_a"],
		fixture.Expected.ScopeIDs["origin"], fixture.Expected.TargetDigests["target"],
		fixture.Expected.PolicyDecisionDigests["target_document_depth_2"],
		fixture.Expected.OutputDigest, fixture.Expected.PublicationID, fixture.Expected.CommitID,
	}
	seen := make(map[string]struct{}, len(digests))
	for _, digest := range digests {
		if _, duplicate := seen[digest]; duplicate {
			t.Fatalf("domain-separated digest collision: %s", digest)
		}
		seen[digest] = struct{}{}
	}
	context := vectorOutputContextValue(t, fixture)
	output := vectorOutputValue(t, fixture)
	before, err := DeriveOutputDigest(context, output)
	if err != nil {
		t.Fatal(err)
	}
	otherPublication := Digest(strings.Repeat("c", 64))
	pageKeyA, err := PageDataKey(Digest(fixture.Expected.PublicationID), output.Page.NormalizedURL)
	if err != nil {
		t.Fatal(err)
	}
	pageKeyB, err := PageDataKey(otherPublication, output.Page.NormalizedURL)
	if err != nil || pageKeyA == pageKeyB {
		t.Fatalf("publication-scoped key setup failed: err=%v", err)
	}
	after, err := DeriveOutputDigest(context, output)
	if err != nil || after != before {
		t.Fatalf("derived publication data fed back into output digest: err=%v", err)
	}
}
