package crawljobsv2

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestTranscriptAmendmentGenesisAndEveryRecordedStart(t *testing.T) {
	chain := newTranscriptAmendmentChain(t, 0, RequestRobots, RequestDocument, RequestRenderResource)
	// The first START may have failed without any network I/O. It still counts:
	// a later successful document cannot silently remove it from the transcript.
	omitted, err := NewDocumentTranscript(chain.context.runPolicy, chain.source, chain.transcript.requests[1])
	if err != nil {
		t.Fatal(err)
	}
	omitted, err = omitted.AppendSuccessfulRequest(chain.transcript.requests[2])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewOutputContext(chain.context.runPolicy, chain.source, omitted, chain.witness, chain.context.renderPolicy); !errors.Is(err, ErrDigestInputMismatch) {
		t.Fatalf("omitted leading START accepted: %v", err)
	}
	// A genuine new claim after earlier cumulative starts has a nonzero B;
	// requiring every transcript to begin at job-wide generation 1 is incorrect.
	nonzero := newTranscriptAmendmentChain(t, 4, RequestDocument, RequestRenderResource)
	if nonzero.context.requestStartsBaseline != 4 || nonzero.context.requestStartsGeneration != 6 ||
		nonzero.context.lease != nonzero.transcript.lease {
		t.Fatal("output context lost the authenticated lease/B/G tuple")
	}
}

func TestTranscriptAmendmentWitnessExactLiveProjection(t *testing.T) {
	chain := newTranscriptAmendmentChain(t, 2, RequestDocument, RequestRenderResource)
	for _, mutation := range []struct {
		name  string
		index int
		value string
	}{
		{"baseline empty", 7, ""}, {"baseline noncanonical", 7, "02"},
		{"baseline equals generation", 7, "4"}, {"baseline exceeds generation", 7, "5"},
		{"generation above budget", 5, "11"}, {"generation zero", 5, "0"},
		{"unleased", 8, "completed"}, {"owner", 9, strings.Repeat("8", 32)},
		{"token", 10, strings.Repeat("9", 64)}, {"lease fence", 11, "3"},
		{"document fence", 1, "1"}, {"active reservation", 12, strings.Repeat("a", 64)},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			raw := append([]string(nil), chain.projection...)
			raw[mutation.index] = mutation.value
			if _, err := newTestTransportAuthority().parseFinalDocumentWitness(chain.context.lease, raw); err == nil {
				t.Fatal("invalid witness accepted")
			}
		})
	}
	for _, raw := range [][]string{chain.projection[:7], chain.projection[:12], append(append([]string(nil), chain.projection...), "")} {
		if _, err := newTestTransportAuthority().parseFinalDocumentWitness(chain.context.lease, raw); !errors.Is(err, ErrResponseArity) {
			t.Fatalf("inexact projection accepted: %v", err)
		}
	}
	for _, mutation := range []struct {
		index int
		value string
	}{{7, "1"}, {5, "3"}} {
		raw := append([]string(nil), chain.projection...)
		raw[mutation.index] = mutation.value
		witness, err := newTestTransportAuthority().parseFinalDocumentWitness(chain.context.lease, raw)
		if err != nil {
			t.Fatalf("well-formed stale tuple: %v", err)
		}
		if _, err := NewOutputContext(chain.context.runPolicy, chain.source, chain.transcript, witness, chain.context.renderPolicy); err == nil {
			t.Fatal("wrong B/G accepted by output context")
		}
	}
}

func TestTranscriptAmendmentCommitFramingAndSemanticStability(t *testing.T) {
	first := newTranscriptAmendmentChain(t, 0, RequestDocument)
	second := newTranscriptAmendmentChain(t, 0, RequestDocument, RequestRenderResource)
	third := newTranscriptAmendmentChain(t, 1, RequestDocument)
	var commits, chunks []Digest
	for _, chain := range []transcriptAmendmentChain{first, second, third} {
		if chain.digest != first.digest || chain.identity.PublicationID != first.identity.PublicationID {
			t.Fatal("transcript-only change altered semantic output/publication")
		}
		identity := chain.identity
		commitID, err := DeriveCommitID(identity)
		if err != nil {
			t.Fatal(err)
		}
		// Independent framing oracle: do not use F, digestFramed, or the
		// production decimal helper to construct the expected digest.
		var preimage []byte
		for _, value := range []string{"mifolyo:crawl-commit:v2", string(identity.RunID), string(identity.JobID),
			strconv.FormatUint(uint64(identity.Fence), 10), string(identity.Token), string(identity.PublicationID),
			strconv.FormatUint(identity.RequestStartsBaseline, 10), strconv.FormatUint(identity.RequestStartsGeneration, 10)} {
			for shift := 56; shift >= 0; shift -= 8 {
				preimage = append(preimage, byte(uint64(len(value))>>shift))
			}
			preimage = append(preimage, []byte(value)...)
		}
		sum := sha256.Sum256(preimage)
		if string(commitID) != hex.EncodeToString(sum[:]) {
			t.Fatal("commit framing/order differs from independent oracle")
		}
		chunk, err := NewPageBlobStageChunk(identity, chain.context, ChunkHTML, chain.output.Page.HTML)
		if err != nil {
			t.Fatal(err)
		}
		chunkDigest, err := DeriveChunkDigest(chunk)
		if err != nil {
			t.Fatal(err)
		}
		commits, chunks = append(commits, commitID), append(chunks, chunkDigest)
	}
	for i := range commits {
		for j := 0; j < i; j++ {
			if commits[i] == commits[j] || chunks[i] == chunks[j] {
				t.Fatal("changed B/G did not change commit and chunk identities")
			}
		}
	}
	for _, tuple := range [][2]uint64{{0, 0}, {1, 1}, {2, 1}, {0, 11}, {10, 10}, {MaxExactInteger, MaxExactInteger}} {
		identity := first.identity
		identity.RequestStartsBaseline, identity.RequestStartsGeneration = tuple[0], tuple[1]
		if _, err := DeriveCommitID(identity); err == nil {
			t.Fatalf("invalid commit tuple %v accepted", tuple)
		}
	}
	identity := first.identity
	identity.RequestStartsBaseline, identity.RequestStartsGeneration = 9, 10
	if _, err := DeriveCommitID(identity); err != nil {
		t.Fatalf("maximum valid tuple: %v", err)
	}
}

func TestTranscriptAmendmentContextCommitChunkAndSealBinding(t *testing.T) {
	chain := newTranscriptAmendmentChain(t, 2, RequestDocument)
	artifacts := newGateArtifacts(t)
	gate, err := NewTransportGate(OperationBeginStage, activeGateInput(artifacts))
	if err != nil {
		t.Fatal(err)
	}
	begin := chain.begin()
	sealGate, err := NewTransportGate(OperationSealStage, activeGateInput(artifacts))
	if err != nil {
		t.Fatal(err)
	}
	blobGate, err := NewTransportGate(OperationStagePageBlob, activeGateInput(artifacts))
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewBeginStageWireRequest(gate, begin)
	if err != nil {
		t.Fatal(err)
	}
	if string(request.semantic[8].Value) != "2" || string(request.semantic[9].Value) != "3" ||
		request.semantic[8].Name != "request_starts_baseline" || request.semantic[9].Name != "request_starts_generation" {
		t.Fatal("BEGIN did not source its ordered B/G suffix from the private context")
	}
	if err := ValidateOutputCommit(chain.context, chain.identity, chain.output); err != nil {
		t.Fatal(err)
	}
	chunk, err := NewPageBlobStageChunk(chain.identity, chain.context, ChunkHTML, chain.output.Page.HTML)
	if err != nil {
		t.Fatal(err)
	}
	seal := SealStageWireInput{Context: chain.context, Lease: chain.context.lease, CommitID: chunk.commitID,
		VerifiedOutputDigest: chain.digest, VerifiedManifestChunkDigest: Digest(strings.Repeat("7", 64))}
	if _, err := NewSealStageWireRequest(sealGate, seal); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct {
		name   string
		mutate func(*CommitIdentity)
	}{
		{"baseline", func(i *CommitIdentity) { i.RequestStartsBaseline-- }},
		{"generation", func(i *CommitIdentity) { i.RequestStartsGeneration++ }},
		{"owner", func(i *CommitIdentity) { i.OwnerID = OwnerID(strings.Repeat("8", 32)) }},
		{"token", func(i *CommitIdentity) { i.Token = LeaseToken(strings.Repeat("9", 64)) }},
		{"fence", func(i *CommitIdentity) { i.Fence++ }},
		{"run", func(i *CommitIdentity) { i.RunID = RunID(strings.Repeat("8", 32)) }},
		{"job", func(i *CommitIdentity) { i.JobID = JobID(strings.Repeat("8", 64)) }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			wrong := chain.identity
			mutation.mutate(&wrong)
			if err := ValidateOutputCommit(chain.context, wrong, chain.output); err == nil {
				t.Fatal("pre-seal accepted wrong identity")
			}
			for _, constructor := range []func() (StageChunk, error){
				func() (StageChunk, error) { return NewPageFieldsStageChunk(wrong, chain.context, chain.output.Page) },
				func() (StageChunk, error) { return NewPageBlobStageChunk(wrong, chain.context, ChunkHTML, nil) },
				func() (StageChunk, error) {
					return NewOutlinksStageChunk(wrong, 0, chain.context, []string{"https://example.com/other"})
				},
				func() (StageChunk, error) { return NewDiscoveriesStageChunk(wrong, 0, chain.context, nil) },
				func() (StageChunk, error) { return NewAliasesStageChunk(wrong, chain.context) },
				func() (StageChunk, error) {
					return NewImagesStageChunk(wrong, chain.context, []OutputImage{{NormalizedSourceURL: "https://example.com/image"}})
				},
				func() (StageChunk, error) { return NewImageManifestStageChunk(wrong, chain.context, nil) },
			} {
				if _, err := constructor(); err == nil {
					t.Fatal("chunk constructor accepted wrong identity")
				}
			}
		})
	}
	for _, mutate := range []func(*LeaseIdentity){
		func(l *LeaseIdentity) { l.OwnerID = OwnerID(strings.Repeat("8", 32)) },
		func(l *LeaseIdentity) { l.Token = LeaseToken(strings.Repeat("9", 64)) },
		func(l *LeaseIdentity) { l.Fence++ },
	} {
		wrong := begin
		mutate(&wrong.Lease)
		if _, err := NewBeginStageWireRequest(gate, wrong); err == nil {
			t.Fatal("BEGIN accepted different full lease")
		}
		if _, err := NewStagePageBlobWireRequest(blobGate, wrong.Lease, chunk); err == nil {
			t.Fatal("chunk wire accepted different full lease")
		}
	}
	wrongSeal := seal
	wrongSeal.Context = newTranscriptAmendmentChain(t, 2, RequestDocument, RequestRenderResource).context
	if _, err := NewSealStageWireRequest(sealGate, wrongSeal); err == nil {
		t.Fatal("seal accepted stale commit generation")
	}
	wrongBegin := begin
	wrongBegin.Context = OutputContext{}
	if _, err := NewBeginStageWireRequest(gate, wrongBegin); err == nil {
		t.Fatal("raw identity manufactured BEGIN authority")
	}
	unbound, err := newValidatedStageChunk(chunk.commitID, chunk.kind, chunk.ordinal, chunk.records)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewStagePageBlobWireRequest(blobGate, chain.context.lease, unbound); err == nil {
		t.Fatal("pure chunk digest manufactured output authority")
	}
	tampered := chunk
	tampered.commitID = Digest(strings.Repeat("7", 64))
	if _, err := NewStagePageBlobWireRequest(blobGate, chain.context.lease, tampered); err == nil {
		t.Fatal("chunk commit/context mismatch accepted")
	}
}

func TestTranscriptAmendmentLiveFreshnessFreezeAndCompletedReplay(t *testing.T) {
	chain := newTranscriptAmendmentChain(t, 2, RequestDocument)
	job := transcriptAmendmentJob(t, chain.context)
	if err := ValidateBeginStageTranscript(job, chain.begin()); err != nil {
		t.Fatalf("first BEGIN: %v", err)
	}
	if err := ValidateRequestTranscriptMutable(job, chain.context.lease); err != nil {
		t.Fatalf("witness read must not freeze: %v", err)
	}
	stale := cloneRecord(job)
	recordAuthoritySet(stale, jobRequestStartsIndex, "4")
	recordAuthoritySet(stale, jobNextRequestOrdinalIndex, "5")
	// All timestamps are identical; only the authenticated counter proves drift.
	if err := ValidateRecord(SchemaJob, stale); err != nil {
		t.Fatal(err)
	}
	if err := ValidateBeginStageTranscript(stale, chain.begin()); err == nil {
		t.Fatal("BEGIN accepted a same-millisecond stale generation")
	}
	stage := transcriptAmendmentStage(t, chain)
	commitID, _ := DeriveCommitID(chain.identity)
	unfrozen := cloneRecord(job)
	recordAuthoritySet(job, jobActiveStageCommitIDIndex, string(commitID))
	recordAuthoritySet(job, jobLastStageCommitIDIndex, string(commitID))
	recordAuthoritySet(job, jobLastStageFenceIndex, "2")
	if err := ValidateJobRequestStartsTransition(OperationBeginStage, unfrozen, job); err != nil {
		t.Fatalf("BEGIN installs freeze while retaining B/G: %v", err)
	}
	for _, operation := range []OperationName{OperationBeginStage, OperationStagePageFields, OperationStagePageBlob,
		OperationStageOutlinksBatch, OperationStageDiscoveriesBatch, OperationStageAliasesBatch,
		OperationStageImagesBatch, OperationStageImageManifest, OperationSealStage, OperationCommit} {
		t.Run(string(operation), func(t *testing.T) {
			if err := ValidateStageTranscript(operation, job, stage, chain.context, chain.identity); err != nil {
				t.Fatalf("valid frozen tuple: %v", err)
			}
			for _, change := range []struct {
				index int
				value string
			}{
				{stageRequestStartsBaselineIndex, "1"}, {stageRequestStartsGenerationIndex, "4"},
				{stageTokenDigestIndex, strings.Repeat("8", 64)}, {stageOwnerIDIndex, strings.Repeat("8", 32)},
				{stageCommitIDIndex, strings.Repeat("8", 64)},
			} {
				wrong := cloneRecord(stage)
				recordAuthoritySet(wrong, change.index, change.value)
				if err := ValidateStageTranscript(operation, job, wrong, chain.context, chain.identity); err == nil {
					t.Fatal("stage tuple substitution accepted")
				}
			}
			wrongJob := cloneRecord(job)
			recordAuthoritySet(wrongJob, jobRequestStartsIndex, "4")
			recordAuthoritySet(wrongJob, jobNextRequestOrdinalIndex, "5")
			if err := ValidateStageTranscript(operation, wrongJob, stage, chain.context, chain.identity); err == nil {
				t.Fatal("live job drift from frozen stage accepted")
			}
		})
	}
	aborted := cloneRecord(job)
	recordAuthoritySet(aborted, jobActiveStageCommitIDIndex, "")
	abortID, _ := DeriveAbortStageTransitionID(AbortStageTransitionInput{Lease: chain.context.lease, CommitID: commitID})
	recordAuthoritySet(aborted, jobLastTransitionIDIndex, string(abortID))
	recordAuthoritySet(aborted, jobLastTransitionStatusIndex, string(StatusStageAborted))
	if err := ValidateJobRequestStartsTransition(OperationAbortStage, job, aborted); err != nil {
		t.Fatalf("abort must retain B/G: %v", err)
	}
	if err := ValidateRequestTranscriptMutable(aborted, chain.context.lease); err == nil {
		t.Fatal("abort reopened request START/RESERVE")
	}
	erasedFreeze := cloneRecord(aborted)
	for _, index := range []int{jobLastStageCommitIDIndex, jobLastTransitionIDIndex, jobLastTransitionStatusIndex} {
		recordAuthoritySet(erasedFreeze, index, "")
	}
	recordAuthoritySet(erasedFreeze, jobLastStageFenceIndex, "0")
	for _, operation := range []OperationName{OperationAbortStage, OperationCleanStage} {
		if err := ValidateJobRequestStartsTransition(operation, aborted, erasedFreeze); err == nil {
			t.Fatal("abort/cleanup erased durable freeze")
		}
	}
	if err := ValidateBeginStageTranscript(aborted, chain.begin()); err == nil {
		t.Fatal("abort allowed restaging on the same fence")
	}
	startedAfterAbort := cloneRecord(aborted)
	recordAuthoritySet(startedAfterAbort, jobRequestStartsIndex, "4")
	if err := ValidateJobRequestStartsTransition(OperationStartRequest, aborted, startedAfterAbort); err == nil {
		t.Fatal("START advanced the frozen post-abort generation")
	}
	completed := cloneRecord(job)
	for _, change := range []struct {
		index int
		value string
	}{
		{jobStateIndex, "completed"}, {jobLeaseOwnerIndex, ""}, {jobLeaseTokenIndex, ""},
		{jobLeaseStartedAtMSIndex, "0"}, {jobLeaseExpiresAtMSIndex, "0"}, {jobLeaseDeliveryStartedIndex, "0"},
		{jobActiveStageCommitIDIndex, ""}, {jobOutputDigestIndex, string(chain.digest)},
		{jobPublicationIDIndex, string(chain.identity.PublicationID)}, {jobCommitIDIndex, string(commitID)},
		{jobLastReasonIndex, string(ReasonPublished)}, {jobCompletedAtMSIndex, "1000"},
	} {
		recordAuthoritySet(completed, change.index, change.value)
	}
	pageKey, _ := PageDataKey(chain.identity.PublicationID, chain.context.finalTarget.CanonicalURL)
	recordAuthoritySet(completed, jobPublishedPageKeyIndex, pageKey)
	if err := ValidateJobRequestStartsTransition(OperationCommit, job, completed); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCompletedCommitReplay(completed, chain.identity); err != nil {
		t.Fatalf("completed replay without stage keys: %v", err)
	}
	for _, tuple := range [][2]uint64{{1, 3}, {2, 4}} {
		wrong := chain.identity
		wrong.RequestStartsBaseline, wrong.RequestStartsGeneration = tuple[0], tuple[1]
		if err := ValidateCompletedCommitReplay(completed, wrong); err == nil {
			t.Fatal("completed replay ignored retained B/G")
		}
	}
}

func TestTranscriptAmendmentRecordBoundsAndBaselineLifecycle(t *testing.T) {
	chain := newTranscriptAmendmentChain(t, 2, RequestDocument)
	job := transcriptAmendmentJob(t, chain.context)
	for _, mutation := range []struct {
		index int
		value string
	}{
		{jobLeaseRequestStartsBaselineIndex, ""}, {jobLeaseRequestStartsBaselineIndex, "02"},
		{jobLeaseRequestStartsBaselineIndex, "4"}, {jobLeaseRequestStartsBaselineIndex, "10"},
		{jobLeaseDeliveryStartedIndex, "0"},
	} {
		wrong := cloneRecord(job)
		recordAuthoritySet(wrong, mutation.index, mutation.value)
		if err := ValidateRecord(SchemaJob, wrong); err == nil {
			t.Fatal("invalid job baseline/delivery relation accepted")
		}
	}
	initial := recordAuthorityJobRecord(t, "ready")
	recordAuthoritySet(initial, jobLeaseRequestStartsBaselineIndex, "1")
	if err := ValidateRecord(SchemaJob, initial); err == nil {
		t.Fatal("initial baseline must be zero")
	}
	stage := transcriptAmendmentStage(t, chain)
	for _, tuple := range [][2]string{{"", "3"}, {"02", "3"}, {"3", "3"}, {"4", "3"}, {"2", "11"}} {
		wrong := cloneRecord(stage)
		recordAuthoritySet(wrong, stageRequestStartsBaselineIndex, tuple[0])
		recordAuthoritySet(wrong, stageRequestStartsGenerationIndex, tuple[1])
		if err := ValidateRecord(SchemaStageMeta, wrong); err == nil {
			t.Fatal("invalid stage tuple accepted")
		}
	}
	clearLease := func(record Record) {
		for _, index := range []int{jobLeaseOwnerIndex, jobLeaseTokenIndex} {
			recordAuthoritySet(record, index, "")
		}
		for _, index := range []int{jobLeaseStartedAtMSIndex, jobLeaseExpiresAtMSIndex, jobLeaseDeliveryStartedIndex} {
			recordAuthoritySet(record, index, "0")
		}
	}
	for _, disposition := range []struct {
		operation     OperationName
		state, reason string
	}{
		{OperationRetry, "delayed", "request_timeout"}, {OperationRecoverExpired, "delayed", "lease_expired_after_io"},
		{OperationCompleteNoOutput, "completed", "already_visited"}, {OperationDead, "dead", "http_4xx"},
		{OperationCancelJob, "cancelled", "operator_cancelled"},
	} {
		t.Run(string(disposition.operation), func(t *testing.T) {
			after := cloneRecord(job)
			clearLease(after)
			recordAuthoritySet(after, jobStateIndex, disposition.state)
			recordAuthoritySet(after, jobLastReasonIndex, disposition.reason)
			switch disposition.state {
			case "delayed":
				recordAuthoritySet(after, jobNotBeforeMSIndex, "2000")
				recordAuthoritySet(after, jobRetryCountIndex, "1")
			case "completed":
				recordAuthoritySet(after, jobCompletedAtMSIndex, "1000")
			case "dead":
				recordAuthoritySet(after, jobDeadAtMSIndex, "1000")
				recordAuthoritySet(after, jobLastFailureReasonIndex, disposition.reason)
			case "cancelled":
				recordAuthoritySet(after, jobCancelledAtMSIndex, "1000")
			}
			if err := ValidateJobRequestStartsTransition(disposition.operation, job, after); err != nil {
				t.Fatalf("baseline retention: %v", err)
			}
			if err := ValidateJobRequestStartsTransition(OperationCleanStage, after, cloneRecord(after)); err != nil {
				t.Fatalf("cleanup retention: %v", err)
			}
			recordAuthoritySet(after, jobLeaseRequestStartsBaselineIndex, "1")
			if err := ValidateJobRequestStartsTransition(disposition.operation, job, after); err == nil {
				t.Fatal("lease-ending operation replaced baseline")
			}
		})
	}
	ready := cloneRecord(job)
	clearLease(ready)
	recordAuthoritySet(ready, jobStateIndex, "ready")
	claimed := cloneRecord(job)
	recordAuthoritySet(claimed, jobClaimCountIndex, "3")
	recordAuthoritySet(claimed, jobLeaseFenceIndex, "3")
	recordAuthoritySet(claimed, jobLeaseRequestStartsBaselineIndex, "3")
	recordAuthoritySet(claimed, jobLeaseDeliveryStartedIndex, "0")
	if err := ValidateJobRequestStartsTransition(OperationTryClaim, ready, claimed); err != nil {
		t.Fatalf("new claim snapshots G into B: %v", err)
	}
	if err := ValidateJobRequestStartsTransition(OperationTryClaim, claimed, cloneRecord(claimed)); err != nil {
		t.Fatalf("claim replay must retain baseline: %v", err)
	}
	preIOReleased := cloneRecord(claimed)
	clearLease(preIOReleased)
	recordAuthoritySet(preIOReleased, jobStateIndex, "ready")
	if err := ValidateJobRequestStartsTransition(OperationReleaseBeforeIO, claimed, preIOReleased); err != nil {
		t.Fatalf("pre-I/O release retains nonzero baseline: %v", err)
	}
	wrong := cloneRecord(claimed)
	recordAuthoritySet(wrong, jobLeaseRequestStartsBaselineIndex, "2")
	recordAuthoritySet(wrong, jobLeaseDeliveryStartedIndex, "1")
	if err := ValidateJobRequestStartsTransition(OperationTryClaim, ready, wrong); err == nil {
		t.Fatal("new claim retained stale prior baseline")
	}
}

type transcriptAmendmentChain struct {
	source     SourceJob
	transcript DocumentTranscript
	witness    FinalDocumentWitness
	projection []string
	context    OutputContext
	identity   CommitIdentity
	output     CrawlOutput
	digest     Digest
}

func (chain transcriptAmendmentChain) begin() BeginStageWireInput {
	return BeginStageWireInput{Context: chain.context, Lease: chain.context.lease, Output: chain.output}
}

func newTranscriptAmendmentChain(t *testing.T, baseline uint64, kinds ...RequestKind) transcriptAmendmentChain {
	t.Helper()
	target := RequestTarget{URLID: recordAuthorityURLID("https://example.com/path"), CanonicalURL: "https://example.com/path"}
	decision, err := NewPolicyDecision(PolicyDecisionInput{RequestKind: RequestDocument, Target: target,
		GroupID: "default", RateScopeID: RateScopeID(strings.Repeat("2", 32)),
		GroupConcurrency: 3, OriginConcurrency: 3, GroupIntervalMS: 100, OriginIntervalMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	lease := LeaseIdentity{RunID: RunID(strings.Repeat("1", 32)), JobID: target.URLID,
		OwnerID: OwnerID(strings.Repeat("3", 32)), Token: LeaseToken(strings.Repeat("4", 64)), Fence: 2}
	group := PolicyGroup{GroupID: decision.GroupID, RateScopeID: decision.RateScopeID, GroupScopeID: decision.GroupScopeID,
		RequestStartLimit: 10, Concurrency: 3, IntervalMS: 100}
	policy := Digest(strings.Repeat("a", 64))
	runPolicy := newAuthenticatedTestRunPolicyAuthority(t, lease.RunID, policy, plainSHA256(testDenyAllRenderPolicyArtifact()), []PolicyGroup{group})
	score, _ := ParseScoreText("0")
	chain := transcriptAmendmentChain{source: SourceJob{JobID: target.URLID, CanonicalURL: target.CanonicalURL,
		ScoreText: score, GroupID: decision.GroupID, RateScopeID: decision.RateScopeID, Decision: decision}}
	transport := newTestTransportAuthority()
	attempts := uint64(1)
	if baseline > 0 {
		attempts = 2 // One prior delivery consumed B starts; this fence adds one.
	}
	for index, kind := range kinds {
		requestTarget := target
		if kind == RequestRobots {
			requestTarget = RequestTarget{URLID: recordAuthorityURLID("https://example.com/robots.txt"), CanonicalURL: "https://example.com/robots.txt"}
		} else if kind == RequestRenderResource {
			requestTarget = RequestTarget{URLID: recordAuthorityURLID("https://example.com/app.js"), CanonicalURL: "https://example.com/app.js"}
		}
		requestDecision := decision
		requestDecision.RequestKind = kind
		requestDecision.TargetURLID = requestTarget.URLID
		requestDecision.TargetDigest, err = DeriveTargetDigest(requestTarget)
		if err != nil {
			t.Fatal(err)
		}
		starts := baseline + uint64(index) + 1
		intent := ReservationIntent{Lease: lease, Target: requestTarget, Decision: requestDecision, CrawlPolicyDigest: policy, RequestOrdinal: starts}
		reservationID, err := DeriveReservationID(runPolicy, intent)
		if err != nil {
			t.Fatal(err)
		}
		response, err := transport.parseStartRequestResponse(runPolicy, intent, []string{
			"STARTED", "1000", string(reservationID), "1000", canonicalDecimal(attempts), canonicalDecimal(starts), canonicalDecimal(starts), canonicalDecimal(starts), "1",
		})
		if err != nil {
			t.Fatal(err)
		}
		permit, err := response.IOPermit()
		if err != nil {
			t.Fatal(err)
		}
		event, err := NewSuccessfulRequest(permit)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			chain.transcript, err = NewRequestTranscript(runPolicy, chain.source, event)
		} else {
			chain.transcript, err = chain.transcript.AppendSuccessfulRequest(event)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	chain.projection = []string{"1000", "2", string(target.URLID), target.CanonicalURL, string(decision.TargetDigest),
		canonicalDecimal(baseline + uint64(len(kinds))), "1000", canonicalDecimal(baseline), "leased", string(lease.OwnerID), string(lease.Token), "2", ""}
	chain.witness, err = transport.parseFinalDocumentWitness(lease, chain.projection)
	if err != nil {
		t.Fatal(err)
	}
	renderPolicy, err := NewRenderPolicyAuthorization(runPolicy, testDenyAllRenderPolicyArtifact())
	if err != nil {
		t.Fatal(err)
	}
	chain.context, err = NewOutputContext(runPolicy, chain.source, chain.transcript, chain.witness, renderPolicy)
	if err != nil {
		t.Fatal(err)
	}
	chain.output = CrawlOutput{Page: OutputPage{NormalizedURL: target.CanonicalURL, HTML: []byte("<html></html>"), ContentType: "text/html", StatusCode: 200}}
	chain.digest, err = DeriveOutputDigest(chain.context, chain.output)
	if err != nil {
		t.Fatal(err)
	}
	publicationID, err := DerivePublicationID(PublicationIdentity{RunID: lease.RunID, JobID: lease.JobID, Fence: lease.Fence, OutputDigest: chain.digest})
	if err != nil {
		t.Fatal(err)
	}
	chain.identity = outputCommitIdentity(chain.context, publicationID)
	return chain
}

func transcriptAmendmentJob(t *testing.T, context OutputContext) Record {
	t.Helper()
	job := recordAuthorityJobRecord(t, "leased")
	for _, change := range []struct {
		index int
		value string
	}{
		{jobClaimCountIndex, "2"}, {jobLeaseFenceIndex, "2"}, {jobRequestStartsIndex, canonicalDecimal(context.requestStartsGeneration)},
		{jobLeaseRequestStartsBaselineIndex, canonicalDecimal(context.requestStartsBaseline)},
		{jobDeliveryAttemptsIndex, canonicalDecimal(context.sourceRequest.deliveryAttempts)},
		{jobNextRequestOrdinalIndex, canonicalDecimal(context.requestStartsGeneration + 3)},
		{jobLeaseExpiresAtMSIndex, "2000"},
		{jobActiveReservationIDIndex, ""}, {jobLastRequestStartedAtMSIndex, "1000"}, {jobUpdatedAtMSIndex, "1000"},
		{jobLastDocumentRequestStartedAtMSIndex, "1000"}, {jobLastDocumentRequestFenceIndex, "2"},
		{jobLastDocumentTargetURLIDIndex, string(context.finalTarget.URLID)}, {jobLastDocumentTargetURLIndex, context.finalTarget.CanonicalURL},
	} {
		recordAuthoritySet(job, change.index, change.value)
	}
	digest, _ := DeriveTargetDigest(context.finalTarget)
	recordAuthoritySet(job, jobLastDocumentTargetDigestIndex, string(digest))
	sourceIntent, err := authenticatedRequestIntent(*context.sourceRequest)
	if err != nil {
		t.Fatal(err)
	}
	decisionDigest, err := DerivePolicyDecisionDigest(sourceIntent.Decision)
	if err != nil {
		t.Fatal(err)
	}
	recordAuthoritySet(job, jobDepthIndex, canonicalDecimal(sourceIntent.Decision.Depth))
	recordAuthoritySet(job, jobPolicyDecisionSHA256Index, string(decisionDigest))
	if err := ValidateRecord(SchemaJob, job); err != nil {
		t.Fatal(err)
	}
	return job
}

func transcriptAmendmentStage(t *testing.T, chain transcriptAmendmentChain) Record {
	t.Helper()
	stage := recordAuthoritySealedStageRecord(t, "0")
	commitID, _ := DeriveCommitID(chain.identity)
	tokenDigest, _ := DeriveTokenDigest(chain.context.lease)
	for _, change := range []struct {
		index int
		value string
	}{
		{stageRunIDIndex, string(chain.identity.RunID)}, {stageJobIDIndex, string(chain.identity.JobID)},
		{stageOwnerIDIndex, string(chain.identity.OwnerID)}, {stageLeaseFenceIndex, "2"},
		{stageTokenDigestIndex, string(tokenDigest)}, {stageCommitIDIndex, string(commitID)},
		{stagePublicationIDIndex, string(chain.identity.PublicationID)}, {stageOutputDigestIndex, string(chain.digest)},
		{stageRequestStartsBaselineIndex, canonicalDecimal(chain.identity.RequestStartsBaseline)},
		{stageRequestStartsGenerationIndex, canonicalDecimal(chain.identity.RequestStartsGeneration)},
		{stageCreatedAtMSIndex, "1000"}, {stageSealedAtMSIndex, "1000"},
		{stageExpiresAtMSIndex, canonicalDecimal(1000 + StageTTLMilliseconds)},
		{stageExpectedOutlinksIndex, "0"}, {stageExpectedDiscoveriesIndex, "0"}, {stageExpectedImagesIndex, "0"},
		{stageOutlinksWrittenIndex, "0"}, {stageDiscoveriesWrittenIndex, "0"}, {stageImagesWrittenIndex, "0"},
		{stageKeyCountIndex, "5"}, {stageOutlinksChunk0DigestIndex, ""}, {stageOutlinksChunk1DigestIndex, ""},
		{stageDiscoveriesChunk0DigestIndex, ""}, {stageImagesChunk0DigestIndex, ""},
	} {
		recordAuthoritySet(stage, change.index, change.value)
	}
	if err := ValidateRecord(SchemaStageMeta, stage); err != nil {
		t.Fatal(err)
	}
	return stage
}
