package crawljobsv2

import (
	"encoding/hex"
	"errors"
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
	reservationIntent := vectorReservationValue(t, fixture, false)
	reservationID, err := DeriveReservationID(reservationIntent)
	if err != nil || string(reservationID) != fixture.Expected.ReservationID {
		t.Fatalf("reservation vector mismatch: err=%v", err)
	}
	alternateOwner, err := ParseOwnerID(fixture.Identities.AlternateOwnerID)
	if err != nil {
		t.Fatal(err)
	}
	reservationIntent.Lease.OwnerID = alternateOwner
	alternateReservationID, err := DeriveReservationID(reservationIntent)
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
			return DeriveRejectReadyTransitionID(RejectReadyTransitionInput{
				RunID: lease.RunID, Job: vectorSourceJobValue(t, fixture, fixture.TryClaim.SourceJobIndex),
				Reason: Reason(reasons["reject_ready"]),
			})
		},
		"try_claim": func() (Digest, error) {
			return DeriveTryClaimTransitionID(TryClaimTransitionInput{
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
	payloads := baselineTransitionPayloads(t, fixture, commitID)
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

func baselineTransitionPayloads(t *testing.T, fixture digestVectorFixture, commitID Digest) map[string]Record {
	t.Helper()
	job := vectorSourceJobValue(t, fixture, fixture.TryClaim.SourceJobIndex)
	jobRecord, err := completeSourceJobRecord(job)
	if err != nil {
		t.Fatal(err)
	}
	intent := vectorReservationValue(t, fixture, true)
	intentFields, err := reservationIntentFields(intent)
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
	commitID, err := DeriveCommitID(CommitIdentity{
		RunID: lease.RunID, JobID: lease.JobID, OwnerID: lease.OwnerID,
		Fence: lease.Fence, Token: lease.Token, PublicationID: publicationID,
	})
	if err != nil || string(commitID) != fixture.Expected.CommitID {
		t.Fatalf("commit vector mismatch: err=%v", err)
	}
	chunkURLs := make([]string, 0, len(fixture.Chunk.Records))
	for _, vector := range fixture.Chunk.Records {
		chunkURLs = append(chunkURLs, vector.TargetURL)
	}
	chunk, err := NewOutlinksStageChunk(commitID, fixture.Chunk.Ordinal, context, chunkURLs)
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
	reservationID, err := DeriveReservationID(intent)
	if err != nil {
		t.Fatal(err)
	}
	startedAt := RedisMilliseconds(fixture.OutputContext.Requests[0].StartedAtMS)
	raw := []string{
		string(StatusStarted), canonicalDecimal(uint64(startedAt)), string(reservationID),
		canonicalDecimal(uint64(startedAt)), "1", "1", "1", "1", "1",
	}
	response, err := ParseStartRequestResponse(intent, raw)
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
	transcript, err := NewDocumentTranscript(source, evidence)
	if err != nil {
		t.Fatal(err)
	}
	targetDigest, err := DeriveTargetDigest(intent.Target)
	if err != nil {
		t.Fatal(err)
	}
	witness, err := ParseFinalDocumentWitness(intent.Lease, []string{
		canonicalDecimal(uint64(startedAt)), canonicalDecimal(uint64(intent.Lease.Fence)),
		string(intent.Target.URLID), intent.Target.CanonicalURL, string(targetDigest),
	})
	if err != nil {
		t.Fatal(err)
	}
	renderPolicy, err := NewRenderPolicyAuthorization(intent.CrawlPolicyDigest, intent.CrawlPolicyDigest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewOutputContext(source, transcript, witness, renderPolicy); err != nil {
		t.Fatalf("valid evidence chain: %v", err)
	}
	otherLease := intent.Lease
	otherLease.OwnerID, err = ParseOwnerID(fixture.Identities.AlternateOwnerID)
	if err != nil {
		t.Fatal(err)
	}
	otherWitness, err := ParseFinalDocumentWitness(otherLease, []string{
		canonicalDecimal(uint64(startedAt)), canonicalDecimal(uint64(otherLease.Fence)),
		string(intent.Target.URLID), intent.Target.CanonicalURL, string(targetDigest),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewOutputContext(source, transcript, otherWitness, renderPolicy); !errors.Is(err, ErrDigestInputMismatch) {
		t.Fatalf("cross-owner evidence error = %v", err)
	}

	raw[2] = fixture.Expected.ReservationID
	if _, err := ParseStartRequestResponse(intent, raw); !errors.Is(err, ErrResponseScalar) {
		t.Fatalf("mismatched reservation evidence error = %v", err)
	}
	if _, err := NewOutputContext(source, DocumentTranscript{}, witness, renderPolicy); !errors.Is(err, ErrDigestInputMismatch) {
		t.Fatalf("uninitialized request evidence error = %v", err)
	}
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
