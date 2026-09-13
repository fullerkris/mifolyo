package crawljobsv2

func outputCommitIdentity(context OutputContext, publicationID Digest) CommitIdentity {
	return CommitIdentity{
		RunID: context.lease.RunID, JobID: context.lease.JobID, OwnerID: context.lease.OwnerID,
		Fence: context.lease.Fence, Token: context.lease.Token, PublicationID: publicationID,
		RequestStartsBaseline: context.requestStartsBaseline, RequestStartsGeneration: context.requestStartsGeneration,
	}
}

func validateOutputCommitIdentity(context OutputContext, identity CommitIdentity) error {
	if err := validateOutputContextAuthority(context); err != nil {
		return err
	}
	if identity != outputCommitIdentity(context, identity.PublicationID) {
		return ErrOutputContextMismatch
	}
	_, err := DeriveCommitID(identity)
	return err
}

// ValidateOutputCommit validates pre-seal output preparation against the same
// authenticated full lease and transcript tuple as its chunks. It does not
// attest to current Redis state; ValidateStageTranscript checks that snapshot.
func ValidateOutputCommit(context OutputContext, identity CommitIdentity, output CrawlOutput) error {
	if err := validateOutputCommitIdentity(context, identity); err != nil {
		return err
	}
	digest, err := DeriveOutputDigest(context, output)
	if err != nil {
		return err
	}
	publicationID, err := DerivePublicationID(PublicationIdentity{
		RunID: identity.RunID, JobID: identity.JobID, Fence: identity.Fence, OutputDigest: digest,
	})
	if err != nil || publicationID != identity.PublicationID {
		return ErrDigestInputMismatch
	}
	return nil
}

// validateFinalPageOutputAuthority performs the context-sensitive portion of
// final-page validation. It is kept separate from record decoding because only
// OutputContext owns the exact run-pinned render authorization.
func validateFinalPageOutputAuthority(record FinalPageRecord, context OutputContext) error {
	if !record.initialized || validateOutputContextAuthority(context) != nil {
		return ErrArtifactMismatch
	}
	fields, err := record.Record()
	if err != nil {
		return ErrArtifactMismatch
	}
	if string(fields[0].Value) != context.finalTarget.CanonicalURL || string(fields[5].Value) != context.lastCrawled {
		return ErrArtifactMismatch
	}

	switch string(fields[6].Value) {
	case "false":
		if len(fields[2].Value) != 0 || len(fields[7].Value) != 0 || len(fields[8].Value) != 0 {
			return ErrArtifactMismatch
		}
	case "true":
		rule := string(fields[7].Value)
		digest := Digest(fields[8].Value)
		if len(fields[2].Value) == 0 || digest != context.renderPolicy.digest {
			return ErrArtifactMismatch
		}
		matchedRule, err := context.renderPolicy.match(context.finalTarget.CanonicalURL)
		if err != nil || !matchedRule.Enabled || matchedRule.ID != rule {
			return ErrArtifactMismatch
		}
	default:
		return ErrArtifactMismatch
	}
	return nil
}
