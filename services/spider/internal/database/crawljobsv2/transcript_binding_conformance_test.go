package crawljobsv2

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type transcriptBindingInput struct {
	OutputContext vectorOutputContext `json:"output_context"`
}

type transcriptBindingExpected struct {
	OutputDigest    string   `json:"output_digest"`
	PublicationID   string   `json:"publication_id"`
	CommitID        string   `json:"commit_id"`
	ChunkDigest     string   `json:"chunk_digest"`
	TerminalWitness []string `json:"terminal_witness"`
}

func TestSharedFixtureV2TranscriptBindingCases(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	for _, vector := range fixture.Cases {
		if vector.Kind == "transcript_binding" {
			t.Run(vector.Name, func(t *testing.T) { verifyTranscriptBindingCase(t, fixture, vector) })
		}
	}
	for _, vector := range fixture.NegativeVectors {
		if vector.Kind == "transcript_binding_mutation" {
			t.Run(vector.Name, func(t *testing.T) {
				actual := runTranscriptBindingNegativeCase(t, fixture, vector)
				if actual != vector.ExpectedRejectionClass {
					t.Fatalf("rejection class = %q, want %q", actual, vector.ExpectedRejectionClass)
				}
			})
		}
	}
}

func verifyTranscriptBindingCase(t *testing.T, fixture digestVectorFixture, vector digestVectorCase) {
	t.Helper()
	input := decodeVectorPart[transcriptBindingInput](t, vector.Input, vector.Name+".input")
	expected := decodeVectorPart[transcriptBindingExpected](t, vector.Expected, vector.Name+".expected")
	fixture.OutputContext = input.OutputContext
	projection := vectorFinalDocumentProjection(t, fixture)
	assertVectorValue(t, projection, expected.TerminalWitness)
	context, err := loadOutputContext(t, fixture, nil, expected.TerminalWitness)
	if err != nil {
		t.Fatalf("authenticate transcript binding: %v", err)
	}
	lease := vectorLease(t, fixture)
	if context.lease != lease || canonicalDecimal(context.requestStartsBaseline) != input.OutputContext.LeaseRequestStartsBaseline ||
		canonicalDecimal(context.requestStartsGeneration) != input.OutputContext.TerminalRequestStartsGeneration {
		t.Fatal("output context did not retain the full lease and explicit B/G binding")
	}
	outputDigest, err := DeriveOutputDigest(context, vectorOutputValue(t, fixture))
	if err != nil {
		t.Fatal(err)
	}
	publicationID, err := DerivePublicationID(PublicationIdentity{
		RunID: lease.RunID, JobID: lease.JobID, Fence: lease.Fence, OutputDigest: outputDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	identity := CommitIdentity{
		RunID: lease.RunID, JobID: lease.JobID, OwnerID: lease.OwnerID, Fence: lease.Fence, Token: lease.Token,
		PublicationID: publicationID, RequestStartsBaseline: context.requestStartsBaseline,
		RequestStartsGeneration: context.requestStartsGeneration,
	}
	commitID, err := DeriveCommitID(identity)
	if err != nil {
		t.Fatal(err)
	}
	outlinks := make([]string, len(fixture.Chunk.Records))
	for index, record := range fixture.Chunk.Records {
		outlinks[index] = record.TargetURL
	}
	chunk, err := NewOutlinksStageChunk(identity, fixture.Chunk.Ordinal, context, outlinks)
	if err != nil {
		t.Fatal(err)
	}
	chunkDigest, err := DeriveChunkDigest(chunk)
	if err != nil {
		t.Fatal(err)
	}
	assertVectorValue(t, transcriptBindingExpected{
		OutputDigest: string(outputDigest), PublicationID: string(publicationID), CommitID: string(commitID),
		ChunkDigest: string(chunkDigest), TerminalWitness: projection,
	}, expected)
	if string(outputDigest) != fixture.Expected.OutputDigest || string(publicationID) != fixture.Expected.PublicationID ||
		string(commitID) == fixture.Expected.CommitID || string(chunkDigest) == fixture.Expected.ChunkDigest {
		t.Fatal("B/G must change commit/chunk identity without changing output/publication")
	}
}

func runTranscriptBindingNegativeCase(t *testing.T, fixture digestVectorFixture, vector digestVectorNegative) string {
	t.Helper()
	input := decodeVectorPart[struct {
		BaseCase string `json:"base_case"`
		Mutation string `json:"mutation"`
	}](t, vector.Input, vector.Name+".input")
	found := false
	for _, base := range fixture.Cases {
		if base.Name == input.BaseCase && base.Kind == "transcript_binding" {
			fixture.OutputContext = decodeVectorPart[transcriptBindingInput](t, base.Input, base.Name+".input").OutputContext
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("unknown transcript binding base %q", input.BaseCase)
	}
	context, err := loadOutputContext(t, fixture, nil, nil)
	if err != nil {
		t.Fatalf("negative fixture's unmodified transcript was rejected: %v", err)
	}
	witness := vectorFinalDocumentProjection(t, fixture)
	requests := fixture.OutputContext.Requests
	switch input.Mutation {
	case "omit_leading":
		fixture.OutputContext.Requests = requests[1:]
	case "omit_middle":
		fixture.OutputContext.Requests = append(requests[:2], requests[3:]...)
	case "omit_trailing":
		fixture.OutputContext.Requests = requests[:len(requests)-1]
	case "duplicate_start_count":
		requests[2].JobRequestStarts = requests[1].JobRequestStarts
	case "skipped_start_count":
		requests[2].JobRequestStarts = "7"
	case "duplicate_ordinal":
		requests[len(requests)-1].RequestOrdinal = requests[len(requests)-2].RequestOrdinal
	case "start_count_exceeds_ordinal":
		requests[0].RequestOrdinal = "3"
	case "baseline_equal_generation":
		witness[7] = fixture.OutputContext.TerminalRequestStartsGeneration
	case "baseline_negative":
		witness[7] = "-1"
	case "baseline_noncanonical":
		witness[7] = "03"
	case "generation_one_over":
		witness[5] = "11"
	case "generation_noncanonical":
		witness[5] = "07"
	case "count_noncanonical":
		requests[0].JobRequestStarts = "04"
	case "ordinal_noncanonical":
		requests[0].RequestOrdinal = "07"
	case "ordinal_one_over":
		requests[len(requests)-1].RequestOrdinal = "101"
	case "old_projection":
		witness = witness[:7]
	case "witness_lease_mismatch":
		witness[9] = fixture.Identities.AlternateOwnerID
	case "witness_active_reservation":
		witness[12] = strings.Repeat("a", 64)
	case "witness_document_timestamp":
		startedAt, err := parseResponseUint(witness[0])
		if err != nil {
			t.Fatal(err)
		}
		witness[0] = canonicalDecimal(startedAt - 1)
	case "stale_generation_binding", "stale_baseline_binding":
		lease := vectorLease(t, fixture)
		identity := CommitIdentity{
			RunID: lease.RunID, JobID: lease.JobID, OwnerID: lease.OwnerID, Fence: lease.Fence, Token: lease.Token,
			PublicationID:         mustFixtureDigest(t, fixture.Expected.PublicationID),
			RequestStartsBaseline: context.requestStartsBaseline, RequestStartsGeneration: context.requestStartsGeneration,
		}
		output := vectorOutputValue(t, fixture)
		if err := ValidateOutputCommit(context, identity, output); err != nil {
			t.Fatalf("unmodified commit binding was rejected: %v", err)
		}
		commitID, err := DeriveCommitID(identity)
		if err != nil {
			t.Fatal(err)
		}
		if input.Mutation == "stale_generation_binding" {
			identity.RequestStartsGeneration--
		} else {
			identity.RequestStartsBaseline--
		}
		staleCommitID, err := DeriveCommitID(identity)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateOutputCommit(context, identity, output); !errors.Is(err, ErrOutputContextMismatch) {
			t.Fatalf("stale commit binding error = %v, want output context mismatch", err)
		}
		return fixtureReplayRejection(commitID, staleCommitID)
	default:
		t.Fatalf("unknown transcript binding mutation %q", input.Mutation)
	}
	_, err = loadOutputContext(t, fixture, nil, witness)
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrResponseArity) {
		return "RESPONSE_ARITY"
	}
	for _, allowed := range []error{
		ErrDigestInputMismatch, ErrInvalidOutput, ErrResponseScalar, ErrInvalidUnsignedDecimal, ErrInvalidResponseAuthority,
	} {
		if errors.Is(err, allowed) {
			return "TRANSCRIPT_BINDING_MISMATCH"
		}
	}
	t.Fatalf("unexpected transcript rejection: %v", err)
	return ""
}

func TestTranscriptBindingFixtureRequiresExplicitSnapshots(t *testing.T) {
	fixture := loadDigestVectorFixture(t)
	raw, err := json.Marshal(fixture.OutputContext)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		"lease_request_starts_baseline", "terminal_request_starts_generation", "job_request_starts", "request_ordinal",
	} {
		for _, replacement := range []json.RawMessage{nil, json.RawMessage("null"), json.RawMessage("1")} {
			t.Run(field+"/"+string(replacement), func(t *testing.T) {
				var object map[string]json.RawMessage
				if err := json.Unmarshal(raw, &object); err != nil {
					t.Fatal(err)
				}
				target := object
				var requests []map[string]json.RawMessage
				if field == "job_request_starts" || field == "request_ordinal" {
					if err := json.Unmarshal(object["requests"], &requests); err != nil {
						t.Fatal(err)
					}
					target = requests[0]
				}
				if replacement == nil {
					delete(target, field)
				} else {
					target[field] = replacement
				}
				if requests != nil {
					object["requests"], err = json.Marshal(requests)
					if err != nil {
						t.Fatal(err)
					}
				}
				mutated, err := json.Marshal(object)
				if err != nil {
					t.Fatal(err)
				}
				var decoded vectorOutputContext
				if err := json.Unmarshal(mutated, &decoded); err == nil {
					t.Fatal("fixture decoder accepted missing or non-string request snapshot")
				}
			})
		}
	}
}
