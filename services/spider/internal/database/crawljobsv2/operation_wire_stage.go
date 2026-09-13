package crawljobsv2

type BeginStageWireInput struct {
	Context OutputContext
	Output  CrawlOutput
	Lease   LeaseIdentity
}

// prepareBeginStage is shared by wire construction and live-record prevalidation.
// No digest, publication, or count can be supplied separately from real output.
func prepareBeginStage(input BeginStageWireInput) (Record, Digest, error) {
	if validateOutputContextAuthority(input.Context) != nil || input.Context.lease != input.Lease {
		return nil, "", ErrOutputContextMismatch
	}
	digest, counts, err := deriveOutputDigestAndCounts(input.Context, input.Output)
	if err != nil {
		return nil, "", err
	}
	publicationID, err := DerivePublicationID(PublicationIdentity{
		RunID: input.Lease.RunID, JobID: input.Lease.JobID, Fence: input.Lease.Fence, OutputDigest: digest,
	})
	if err != nil {
		return nil, "", err
	}
	commitID, err := DeriveCommitID(outputCommitIdentity(input.Context, publicationID))
	if err != nil {
		return nil, "", err
	}
	semantic, _ := operationWireLeaseFields(input.Lease)
	semantic = append(semantic,
		textField("commit_id", string(commitID)),
		textField("publication_id", string(publicationID)),
		textField("output_digest", string(digest)),
		textField("request_starts_baseline", canonicalDecimal(input.Context.requestStartsBaseline)),
		textField("request_starts_generation", canonicalDecimal(input.Context.requestStartsGeneration)),
		textField("expected_page_fields", canonicalDecimal(counts[0])),
		textField("expected_outlinks", canonicalDecimal(counts[1])),
		textField("expected_discoveries", canonicalDecimal(counts[2])),
		textField("expected_aliases", canonicalDecimal(counts[3])),
		textField("expected_images", canonicalDecimal(counts[4])),
	)
	return semantic, commitID, nil
}

func NewBeginStageWireRequest(gate TransportGate, input BeginStageWireInput) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationBeginStage, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	semantic, commitID, err := prepareBeginStage(input)
	if err != nil {
		return OperationWireRequest{}, err
	}
	return newOperationWireRequest(
		OperationBeginStage, gatePointer, semantic, nil, nil,
		operationWireKeyContext{runID: input.Lease.RunID, jobID: input.Lease.JobID, commitID: commitID},
		operationWireChunkContext{},
	)
}

func NewStagePageFieldsWireRequest(gate TransportGate, lease LeaseIdentity, chunk StageChunk) (OperationWireRequest, error) {
	return newStageChunkWireRequest(OperationStagePageFields, gate, lease, chunk)
}

func NewStagePageBlobWireRequest(gate TransportGate, lease LeaseIdentity, chunk StageChunk) (OperationWireRequest, error) {
	return newStageChunkWireRequest(OperationStagePageBlob, gate, lease, chunk)
}

func NewStageOutlinksBatchWireRequest(gate TransportGate, lease LeaseIdentity, chunk StageChunk) (OperationWireRequest, error) {
	return newStageChunkWireRequest(OperationStageOutlinksBatch, gate, lease, chunk)
}

func NewStageDiscoveriesBatchWireRequest(gate TransportGate, lease LeaseIdentity, chunk StageChunk) (OperationWireRequest, error) {
	return newStageChunkWireRequest(OperationStageDiscoveriesBatch, gate, lease, chunk)
}

func NewStageAliasesBatchWireRequest(gate TransportGate, lease LeaseIdentity, chunk StageChunk) (OperationWireRequest, error) {
	return newStageChunkWireRequest(OperationStageAliasesBatch, gate, lease, chunk)
}

func NewStageImagesBatchWireRequest(gate TransportGate, lease LeaseIdentity, chunk StageChunk) (OperationWireRequest, error) {
	return newStageChunkWireRequest(OperationStageImagesBatch, gate, lease, chunk)
}

func NewStageImageManifestWireRequest(gate TransportGate, lease LeaseIdentity, chunk StageChunk) (OperationWireRequest, error) {
	return newStageChunkWireRequest(OperationStageImageManifest, gate, lease, chunk)
}

func NewAbortStageWireRequest(gate TransportGate, input AbortStageTransitionInput) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationAbortStage, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if validateLeaseIdentity(input.Lease) != nil || validateNonzeroDigest(input.CommitID) != nil {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	transitionID, err := DeriveAbortStageTransitionID(input)
	if err != nil {
		return OperationWireRequest{}, err
	}
	semantic, _ := operationWireLeaseFields(input.Lease)
	semantic = append(semantic,
		textField("commit_id", string(input.CommitID)),
		textField("transition_id", string(transitionID)),
	)
	return newOperationWireRequest(
		OperationAbortStage, gatePointer, semantic, nil, nil,
		operationWireKeyContext{runID: input.Lease.RunID, jobID: input.Lease.JobID, commitID: input.CommitID},
		operationWireChunkContext{},
	)
}

type SealStageWireInput struct {
	Context                     OutputContext
	Lease                       LeaseIdentity
	CommitID                    Digest
	VerifiedOutputDigest        Digest
	VerifiedManifestChunkDigest Digest
}

func NewSealStageWireRequest(gate TransportGate, input SealStageWireInput) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationSealStage, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if validateLeaseIdentity(input.Lease) != nil || validateNonzeroDigest(input.CommitID) != nil ||
		validateNonzeroDigest(input.VerifiedOutputDigest) != nil || validateNonzeroDigest(input.VerifiedManifestChunkDigest) != nil {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	if validateOutputContextAuthority(input.Context) != nil || input.Context.lease != input.Lease {
		return OperationWireRequest{}, ErrOutputContextMismatch
	}
	publicationID, err := DerivePublicationID(PublicationIdentity{
		RunID: input.Lease.RunID, JobID: input.Lease.JobID, Fence: input.Lease.Fence, OutputDigest: input.VerifiedOutputDigest,
	})
	if err != nil {
		return OperationWireRequest{}, err
	}
	commitID, err := DeriveCommitID(outputCommitIdentity(input.Context, publicationID))
	if err != nil || commitID != input.CommitID {
		return OperationWireRequest{}, ErrOutputContextMismatch
	}
	semantic, _ := operationWireLeaseFields(input.Lease)
	semantic = append(semantic,
		textField("commit_id", string(input.CommitID)),
		textField("verified_output_digest", string(input.VerifiedOutputDigest)),
		textField("verified_manifest_chunk_digest", string(input.VerifiedManifestChunkDigest)),
	)
	return newOperationWireRequest(
		OperationSealStage, gatePointer, semantic, nil, nil,
		operationWireKeyContext{runID: input.Lease.RunID, jobID: input.Lease.JobID, commitID: input.CommitID},
		operationWireChunkContext{},
	)
}

func NewCommitWireRequest(gate TransportGate, identity CommitIdentity) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(OperationCommit, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	commitID, err := DeriveCommitID(identity)
	if err != nil {
		return OperationWireRequest{}, err
	}
	lease := LeaseIdentity{
		RunID: identity.RunID, JobID: identity.JobID, OwnerID: identity.OwnerID, Fence: identity.Fence, Token: identity.Token,
	}
	semantic, _ := operationWireLeaseFields(lease)
	semantic = append(semantic, textField("commit_id", string(commitID)))
	return newOperationWireRequest(
		OperationCommit, gatePointer, semantic, nil, nil,
		operationWireKeyContext{runID: identity.RunID, jobID: identity.JobID, commitID: commitID},
		operationWireChunkContext{},
	)
}

func newStageChunkWireRequest(operation OperationName, gate TransportGate, lease LeaseIdentity, chunk StageChunk) (OperationWireRequest, error) {
	gatePointer, err := operationWireGate(operation, gate)
	if err != nil {
		return OperationWireRequest{}, err
	}
	if validateLeaseIdentity(lease) != nil || validateNonzeroDigest(chunk.commitID) != nil {
		return OperationWireRequest{}, ErrOperationWireArguments
	}
	if err := validateStageChunkAuthority(chunk, lease); err != nil {
		return OperationWireRequest{}, err
	}
	digest, err := DeriveChunkDigest(chunk)
	if err != nil {
		return OperationWireRequest{}, err
	}
	semantic, _ := operationWireLeaseFields(lease)
	semantic = append(semantic,
		textField("commit_id", string(chunk.commitID)),
		textField("chunk_kind", string(chunk.kind)),
		textField("chunk_ordinal", canonicalDecimal(chunk.ordinal)),
		textField("chunk_digest", string(digest)),
		textField("record_count", canonicalDecimal(uint64(len(chunk.records)))),
	)
	return newOperationWireRequest(
		operation,
		gatePointer,
		semantic,
		chunk.records,
		nil,
		operationWireKeyContext{runID: lease.RunID, jobID: lease.JobID, commitID: chunk.commitID},
		operationWireChunkContext{present: true, kind: chunk.kind, ordinal: chunk.ordinal, digest: digest, commitID: chunk.commitID},
	)
}
