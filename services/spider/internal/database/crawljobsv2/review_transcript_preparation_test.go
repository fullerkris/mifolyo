package crawljobsv2

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestReviewBeginRequiresRealOutputAndDerivesEveryField(t *testing.T) {
	inputType := reflect.TypeOf(BeginStageWireInput{})
	wantFields := []string{"Context", "Output", "Lease"}
	if inputType.NumField() != len(wantFields) {
		t.Fatal("BEGIN exposes an independent count/digest/publication override")
	}
	for index, name := range wantFields {
		if inputType.Field(index).Name != name {
			t.Fatal("BEGIN input API drifted from the joint output boundary")
		}
	}
	f := newWireOracleFixture(t)
	gate := wireOracleGate(t, f, OperationBeginStage, wireOracleActive)
	input := BeginStageWireInput{Context: f.outputContext, Lease: f.lease, Output: f.output}
	request, err := NewBeginStageWireRequest(gate, input)
	if err != nil {
		t.Fatal(err)
	}
	// f's digest, publication, and commit are calculated independently from
	// literal semantic records, not by the preparation or production hash APIs.
	want := []string{string(f.commitID), string(f.publicationID), string(f.outputDigest), "2", "3", "10", "1", "1", "1", "1"}
	for index, value := range want {
		if string(request.semantic[index+5].Value) != value {
			t.Fatalf("derived BEGIN field %s differs from semantic oracle", request.semantic[index+5].Name)
		}
	}
	// Three starts, including a repeated alias and a resource, represent ONE
	// canonical alias. The count must not use request count or G.
	chain := newTranscriptAmendmentChain(t, 0, RequestDocument, RequestRedirect, RequestRenderResource)
	input = chain.begin()
	input.Output.Outlinks = []string{"https://example.com/c", "https://example.com/b", "https://example.com/a"}
	input.Output.Images = []OutputImage{{NormalizedSourceURL: "https://example.com/i2"}, {NormalizedSourceURL: "https://example.com/i1"}}
	request, err = NewBeginStageWireRequest(gate, input)
	if err != nil {
		t.Fatal(err)
	}
	wantCounts := []string{"10", "3", "0", "1", "2"}
	for index, value := range wantCounts {
		if string(request.semantic[index+10].Value) != value {
			t.Fatal("BEGIN count is not the canonical section count")
		}
	}
	sections := make([][]Record, 5)
	sections[0] = []Record{{wireOracleField("normalized_url", "https://example.com/path"), wireOracleField("html", "<html></html>"),
		wireOracleField("original_html", ""), wireOracleField("content_type", "text/html"), wireOracleField("status_code", "200"),
		wireOracleField("last_crawled", "Thu, 01 Jan 1970 00:00:01 UTC"), wireOracleField("rendered", "false"),
		wireOracleField("render_policy_rule", ""), wireOracleField("render_policy_sha256", "")}}
	for _, url := range []string{"https://example.com/a", "https://example.com/b", "https://example.com/c"} {
		sections[1] = append(sections[1], Record{wireOracleField("target_url", url)})
	}
	for _, url := range []string{"https://example.com/i1", "https://example.com/i2"} {
		sections[2] = append(sections[2], Record{wireOracleField("normalized_source_url", url), wireOracleField("alt", "")})
	}
	sections[4] = []Record{{wireOracleField("url_id", string(chain.source.JobID)), wireOracleField("canonical_url", chain.source.CanonicalURL), wireOracleField("depth", "0")}}
	digest := wireOracleOutputDigest(sections)
	publication := wireOracleFramedDigest("mifolyo:page-publication:v2", string(input.Lease.RunID), string(input.Lease.JobID), "2", string(digest))
	commit := wireOracleFramedDigest("mifolyo:crawl-commit:v2", string(input.Lease.RunID), string(input.Lease.JobID), "2", string(input.Lease.Token), string(publication), "0", "3")
	for index, value := range []Digest{commit, publication, digest} {
		if string(request.semantic[index+5].Value) != string(value) {
			t.Fatal("BEGIN hashes differ from independently encoded canonical sections")
		}
	}
	if err := ValidateBeginStageTranscript(transcriptAmendmentJob(t, chain.context), input); err != nil {
		t.Fatalf("live BEGIN uses the same real-output preparation: %v", err)
	}
}

func TestReviewBeginRejectsInvalidOutputAtBothBoundaries(t *testing.T) {
	chain := newTranscriptAmendmentChain(t, 0, RequestDocument)
	gate, err := NewTransportGate(OperationBeginStage, activeGateInput(newGateArtifacts(t)))
	if err != nil {
		t.Fatal(err)
	}
	job := transcriptAmendmentJob(t, chain.context)
	for _, mutation := range []struct {
		name  string
		apply func(*BeginStageWireInput)
	}{
		{"empty output", func(i *BeginStageWireInput) { i.Output = CrawlOutput{} }},
		{"wrong final URL", func(i *BeginStageWireInput) { i.Output.Page.NormalizedURL = "https://example.com/other" }},
		{"invalid HTML", func(i *BeginStageWireInput) { i.Output.Page.HTML = []byte{0xff} }},
		{"invalid status", func(i *BeginStageWireInput) { i.Output.Page.StatusCode = 500 }},
		{"duplicate outlinks", func(i *BeginStageWireInput) {
			i.Output.Outlinks = []string{"https://example.com/a", "https://example.com/a"}
		}},
		{"self outlink", func(i *BeginStageWireInput) { i.Output.Outlinks = []string{i.Output.Page.NormalizedURL} }},
		{"duplicate images", func(i *BeginStageWireInput) {
			i.Output.Images = []OutputImage{{NormalizedSourceURL: "https://example.com/i"}, {NormalizedSourceURL: "https://example.com/i"}}
		}},
		{"invalid discovery", func(i *BeginStageWireInput) { i.Output.Discoveries = []OutputDiscovery{{}} }},
		{"raw context", func(i *BeginStageWireInput) { i.Context = OutputContext{} }},
		{"wrong lease", func(i *BeginStageWireInput) { i.Lease.OwnerID = OwnerID(strings.Repeat("8", 32)) }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			input := chain.begin()
			mutation.apply(&input)
			if _, err := NewBeginStageWireRequest(gate, input); err == nil {
				t.Fatal("wire BEGIN accepted invalid joint output/context")
			}
			if err := ValidateBeginStageTranscript(job, input); err == nil {
				t.Fatal("record BEGIN bypassed joint preparation")
			}
		})
	}
}

func TestReviewTranscriptChecksExactWitnessAndAdmittedSource(t *testing.T) {
	chain := newTranscriptAmendmentChain(t, 0, RequestDocument)
	base := transcriptAmendmentJob(t, chain.context)
	stage := transcriptAmendmentStage(t, chain)
	commitID, _ := DeriveCommitID(chain.identity)
	for _, mutation := range []struct {
		name  string
		apply func(Record)
	}{
		{"final target", func(job Record) {
			target := mustFixtureTargetForURL(t, "https://example.com/different-final")
			digest, _ := DeriveTargetDigest(target)
			recordAuthoritySet(job, jobLastDocumentTargetURLIDIndex, string(target.URLID))
			recordAuthoritySet(job, jobLastDocumentTargetURLIndex, target.CanonicalURL)
			recordAuthoritySet(job, jobLastDocumentTargetDigestIndex, string(digest))
		}},
		{"subsecond document and terminal timestamps", func(job Record) {
			recordAuthoritySet(job, jobLastDocumentRequestStartedAtMSIndex, "1001")
			recordAuthoritySet(job, jobLastRequestStartedAtMSIndex, "1001")
			recordAuthoritySet(job, jobUpdatedAtMSIndex, "1001")
		}},
		{"terminal timestamp only", func(job Record) {
			recordAuthoritySet(job, jobLastRequestStartedAtMSIndex, "1001")
			recordAuthoritySet(job, jobUpdatedAtMSIndex, "1001")
		}},
		{"admitted depth and decision", func(job Record) {
			decision := chain.source.Decision
			decision.Depth++
			digest, _ := DerivePolicyDecisionDigest(decision)
			recordAuthoritySet(job, jobDepthIndex, "1")
			recordAuthoritySet(job, jobPolicyDecisionSHA256Index, string(digest))
		}},
		{"source group", func(job Record) { recordAuthoritySet(job, jobGroupIDIndex, "other") }},
		{"admitted source URL and origin", func(job Record) {
			target := mustFixtureTargetForURL(t, "https://other.example.com/source")
			origin, _ := DeriveCanonicalOrigin(target.CanonicalURL)
			scope, _ := DeriveOriginScopeID(origin)
			recordAuthoritySet(job, jobJobIDIndex, string(target.URLID))
			recordAuthoritySet(job, jobURLIDIndex, string(target.URLID))
			recordAuthoritySet(job, jobCanonicalURLIndex, target.CanonicalURL)
			recordAuthoritySet(job, jobInitialOriginScopeIDIndex, string(scope))
		}},
		{"source rate and scope", func(job Record) {
			rate := RateScopeID(strings.Repeat("8", 32))
			scope, _ := DeriveGroupScopeID(rate)
			recordAuthoritySet(job, jobRateScopeIDIndex, string(rate))
			recordAuthoritySet(job, jobGroupScopeIDIndex, string(scope))
		}},
		{"source decision", func(job Record) { recordAuthoritySet(job, jobPolicyDecisionSHA256Index, strings.Repeat("8", 64)) }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			job := cloneRecord(base)
			mutation.apply(job)
			if err := ValidateRecord(SchemaJob, job); err != nil {
				t.Fatalf("probe must be independently well-formed: %v", err)
			}
			if err := ValidateBeginStageTranscript(job, chain.begin()); !errors.Is(err, ErrRecordRelation) {
				t.Fatalf("changed snapshot accepted by BEGIN: %v", err)
			}
			recordAuthoritySet(job, jobActiveStageCommitIDIndex, string(commitID))
			recordAuthoritySet(job, jobLastStageCommitIDIndex, string(commitID))
			recordAuthoritySet(job, jobLastStageFenceIndex, "2")
			for _, operation := range []OperationName{OperationBeginStage, OperationStagePageFields, OperationStagePageBlob,
				OperationStageOutlinksBatch, OperationStageDiscoveriesBatch, OperationStageAliasesBatch,
				OperationStageImagesBatch, OperationStageImageManifest, OperationSealStage, OperationCommit} {
				if err := ValidateStageTranscript(operation, job, stage, chain.context, chain.identity); !errors.Is(err, ErrRecordRelation) {
					t.Fatalf("%s accepted changed exact witness/source: %v", operation, err)
				}
			}
		})
	}
}

func TestReviewFrozenJobRequiresAbortAndRetainedStartedInterval(t *testing.T) {
	job, _, _ := reviewPostAbortBackpressureJob(t)
	for _, change := range []struct {
		index int
		value string
	}{
		{jobCommitBackpressureFenceIndex, "0"}, {jobCommitBackpressureReasonIndex, "none"},
		{jobCommitBackpressureStartedAtMSIndex, "0"}, {jobCommitBackpressureDeadlineMSIndex, "0"},
	} {
		recordAuthoritySet(job, change.index, change.value)
	}
	if err := ValidateRecord(SchemaJob, job); err != nil {
		t.Fatalf("exact abort without backpressure: %v", err)
	}
	for _, status := range []string{"", string(StatusClaimed), string(StatusStageAborted)} {
		wrong := cloneRecord(job)
		recordAuthoritySet(wrong, jobLastTransitionStatusIndex, status)
		id := strings.Repeat("a", 64)
		if status == "" {
			id = ""
		}
		recordAuthoritySet(wrong, jobLastTransitionIDIndex, id)
		if err := ValidateRecord(SchemaJob, wrong); !errors.Is(err, ErrRecordRelation) {
			t.Fatalf("unproven post-abort freeze accepted: %v", err)
		}
	}
	cleared := cloneRecord(job)
	reviewClearLeaseAndBackpressure(cleared)
	recordAuthoritySet(cleared, jobStateIndex, "cancelled")
	recordAuthoritySet(cleared, jobLastReasonIndex, string(ReasonOperatorCancelled))
	recordAuthoritySet(cleared, jobCancelledAtMSIndex, "200")
	reviewSetJobTransition(cleared, Digest(strings.Repeat("a", 64)), StatusCancelled)
	if err := ValidateRecord(SchemaJob, cleared); err != nil {
		t.Fatal(err)
	}
	recordAuthoritySet(cleared, jobLeaseRequestStartsBaselineIndex, "1") // B=G, with a current-fence stage.
	if err := ValidateRecord(SchemaJob, cleared); !errors.Is(err, ErrRecordRelation) {
		t.Fatalf("cleared lease erased frozen G>B: %v", err)
	}
	// A later pre-I/O claim may retain an older stage, with B=G and no current
	// abort evidence. The freeze must be scoped to its recorded fence.
	later := cloneRecord(job)
	recordAuthoritySet(later, jobClaimCountIndex, "2")
	recordAuthoritySet(later, jobLeaseFenceIndex, "2")
	recordAuthoritySet(later, jobLeaseRequestStartsBaselineIndex, "1")
	recordAuthoritySet(later, jobLeaseDeliveryStartedIndex, "0")
	recordAuthoritySet(later, jobNextRequestOrdinalIndex, "3")
	reviewSetJobTransition(later, Digest(strings.Repeat("b", 64)), StatusClaimed)
	if err := ValidateRecord(SchemaJob, later); err != nil {
		t.Fatalf("older residual stage rejected under later claim: %v", err)
	}
}

func TestReviewOutputContextOwnsExactEvidenceSnapshots(t *testing.T) {
	chain := newTranscriptAmendmentChain(t, 0, RequestDocument)
	job := transcriptAmendmentJob(t, chain.context)
	chain.witness.redisStartedAtMS++
	chain.witness.terminalRequestStartedAtMS++
	chain.source.Depth++
	chain.source.Decision.Depth++
	chain.transcript.requests[0].redisStartedAtMS++
	chain.projection[0] = "1001"
	if err := ValidateBeginStageTranscript(job, chain.begin()); err != nil {
		t.Fatalf("caller evidence/DTO copies altered private context snapshots: %v", err)
	}
}

func TestReviewWorkerReservationTerminalIsStrictlyBeforeExpiry(t *testing.T) {
	for _, state := range []string{"finished", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			record := recordAuthorityReservationRecord(t, state)
			recordAuthoritySet(record, reservationTerminalAtMSIndex, "999")
			if err := ValidateRecord(SchemaReservation, record); err != nil {
				t.Fatal(err)
			}
			recordAuthoritySet(record, reservationTerminalAtMSIndex, "1000")
			if err := ValidateRecord(SchemaReservation, record); !errors.Is(err, ErrRecordRelation) {
				t.Fatalf("worker terminal at expiry accepted: %v", err)
			}
		})
	}
	expired := recordAuthorityReservationRecord(t, "expired")
	if err := ValidateRecord(SchemaReservation, expired); err != nil {
		t.Fatal(err)
	}
	recordAuthoritySet(expired, reservationTerminalAtMSIndex, "999")
	if err := ValidateRecord(SchemaReservation, expired); err == nil {
		t.Fatal("early recovery accepted")
	}
}

func TestReviewPriorDeliveriesRespectClaimAdmission(t *testing.T) {
	for _, tuple := range []struct {
		name                  string
		b, g, attempts, fence uint64
		valid                 bool
	}{
		{"first-unstarted-fence", 0, 0, 0, 1, true},
		{"second-unstarted-fence", 1, 1, 1, 2, true},
		{"third-unstarted-fence", 2, 2, 2, 3, true},
		{"genuine-third-delivery", 2, 3, 3, 3, true},
		{"delivery-before-first-fence", 1, 1, 1, 1, false},
		{"claim-after-delivery-exhaustion", 3, 3, 3, 4, false},
		{"multiple-starts-after-delivery-exhaustion", 9, 9, 3, 4, false},
	} {
		t.Run(tuple.name, func(t *testing.T) {
			job := recordAuthorityJobRecord(t, "leased")
			recordAuthoritySet(job, jobClaimCountIndex, strconv.FormatUint(tuple.fence, 10))
			recordAuthoritySet(job, jobLeaseFenceIndex, strconv.FormatUint(tuple.fence, 10))
			recordAuthoritySet(job, jobNextRequestOrdinalIndex, "10")
			recordAuthoritySet(job, jobLeaseRequestStartsBaselineIndex, strconv.FormatUint(tuple.b, 10))
			recordAuthoritySet(job, jobRequestStartsIndex, strconv.FormatUint(tuple.g, 10))
			recordAuthoritySet(job, jobDeliveryAttemptsIndex, strconv.FormatUint(tuple.attempts, 10))
			if tuple.g == 0 {
				recordAuthoritySet(job, jobLastRequestStartedAtMSIndex, "0")
			}
			if tuple.b == tuple.g {
				recordAuthoritySet(job, jobLeaseDeliveryStartedIndex, "0")
			}
			for _, clearLease := range []bool{false, true} {
				if clearLease {
					reviewClearLeaseAndBackpressure(job)
					recordAuthoritySet(job, jobStateIndex, "cancelled")
					recordAuthoritySet(job, jobLastReasonIndex, string(ReasonOperatorCancelled))
					recordAuthoritySet(job, jobCancelledAtMSIndex, "200")
				}
				if err := ValidateRecord(SchemaJob, job); (err == nil) != tuple.valid {
					t.Fatalf("prior delivery validity=%t, clear=%t: %v", tuple.valid, clearLease, err)
				}
			}
		})
	}
}

func TestReviewDeliveryAttemptsMatchBaselineIntervals(t *testing.T) {
	for _, tuple := range []struct {
		b, g, attempts uint64
		valid          bool
	}{
		{2, 3, 1, false}, {2, 3, 2, true}, {2, 3, 3, true}, {1, 5, 3, false}, {1, 5, 2, true},
		{0, 5, 1, true}, {0, 5, 2, false}, {0, 5, 3, false}, {0, 0, 0, true}, {0, 0, 1, false},
		{1, 1, 1, true}, {2, 2, 1, true}, {2, 2, 2, true}, {2, 2, 3, false},
	} {
		name := strconv.FormatUint(tuple.b, 10) + "/" + strconv.FormatUint(tuple.g, 10) + "/" + strconv.FormatUint(tuple.attempts, 10)
		t.Run(name, func(t *testing.T) {
			job := recordAuthorityJobRecord(t, "leased")
			recordAuthoritySet(job, jobClaimCountIndex, "3")
			recordAuthoritySet(job, jobLeaseFenceIndex, "3")
			recordAuthoritySet(job, jobNextRequestOrdinalIndex, "10")
			recordAuthoritySet(job, jobLeaseRequestStartsBaselineIndex, strconv.FormatUint(tuple.b, 10))
			recordAuthoritySet(job, jobRequestStartsIndex, strconv.FormatUint(tuple.g, 10))
			recordAuthoritySet(job, jobDeliveryAttemptsIndex, strconv.FormatUint(tuple.attempts, 10))
			if tuple.g == 0 {
				recordAuthoritySet(job, jobLastRequestStartedAtMSIndex, "0")
			}
			if tuple.b == tuple.g {
				recordAuthoritySet(job, jobLeaseDeliveryStartedIndex, "0")
			}
			for _, clearLease := range []bool{false, true} {
				if clearLease {
					reviewClearLeaseAndBackpressure(job)
					recordAuthoritySet(job, jobStateIndex, "cancelled")
					recordAuthoritySet(job, jobLastReasonIndex, string(ReasonOperatorCancelled))
					recordAuthoritySet(job, jobCancelledAtMSIndex, "200")
				}
				if err := ValidateRecord(SchemaJob, job); (err == nil) != tuple.valid {
					t.Fatalf("delivery interval validity=%t, clear=%t: %v", tuple.valid, clearLease, err)
				}
			}
		})
	}
	chain := newTranscriptAmendmentChain(t, 2, RequestDocument)
	for _, attempts := range []uint64{1, 3} { // Too few prior deliveries, or more deliveries than claim fences.
		event := newTestSuccessfulStartEventWithAttempts(t, chain.context.runPolicy, chain.source, chain.context.lease,
			Digest(strings.Repeat("a", 64)), RequestDocument, chain.context.finalTarget, 3, 1000, 3, 3, 3, attempts)
		transcript, err := NewDocumentTranscript(chain.context.runPolicy, chain.source, event)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := NewOutputContext(chain.context.runPolicy, chain.source, transcript, chain.witness, chain.context.renderPolicy); !errors.Is(err, ErrDigestInputMismatch) {
			t.Fatalf("output context accepted an impossible prior/current delivery history: %v", err)
		}
	}
}
