package crawljobsv2

const redactedValue = "<redacted>"

func redactedString(typeName string) string {
	return "crawljobsv2." + typeName + "{" + redactedValue + "}"
}

func redactedJSON(typeName string) ([]byte, error) {
	return []byte(`{"type":"crawljobsv2.` + typeName + `","redacted":true}`), nil
}

func (RunID) String() string                       { return redactedValue }
func (RunID) GoString() string                     { return redactedValue }
func (RunID) MarshalJSON() ([]byte, error)         { return redactedJSON("RunID") }
func (JobID) String() string                       { return redactedValue }
func (JobID) GoString() string                     { return redactedValue }
func (JobID) MarshalJSON() ([]byte, error)         { return redactedJSON("JobID") }
func (OwnerID) String() string                     { return redactedValue }
func (OwnerID) GoString() string                   { return redactedValue }
func (OwnerID) MarshalJSON() ([]byte, error)       { return redactedJSON("OwnerID") }
func (LeaseToken) String() string                  { return redactedValue }
func (LeaseToken) GoString() string                { return redactedValue }
func (LeaseToken) MarshalJSON() ([]byte, error)    { return redactedJSON("LeaseToken") }
func (Digest) String() string                      { return redactedValue }
func (Digest) GoString() string                    { return redactedValue }
func (Digest) MarshalJSON() ([]byte, error)        { return redactedJSON("Digest") }
func (ReservationID) String() string               { return redactedValue }
func (ReservationID) GoString() string             { return redactedValue }
func (ReservationID) MarshalJSON() ([]byte, error) { return redactedJSON("ReservationID") }
func (RateScopeID) String() string                 { return redactedValue }
func (RateScopeID) GoString() string               { return redactedValue }
func (RateScopeID) MarshalJSON() ([]byte, error)   { return redactedJSON("RateScopeID") }
func (GroupID) String() string                     { return redactedValue }
func (GroupID) GoString() string                   { return redactedValue }
func (GroupID) MarshalJSON() ([]byte, error)       { return redactedJSON("GroupID") }
func (CanonicalOrigin) String() string             { return redactedValue }
func (CanonicalOrigin) GoString() string           { return redactedValue }
func (CanonicalOrigin) MarshalJSON() ([]byte, error) {
	return redactedJSON("CanonicalOrigin")
}

func (Field) String() string               { return redactedString("Field") }
func (Field) GoString() string             { return redactedString("Field") }
func (Field) MarshalJSON() ([]byte, error) { return redactedJSON("Field") }
func (Record) String() string              { return redactedString("Record") }
func (Record) GoString() string            { return redactedString("Record") }
func (Record) MarshalJSON() ([]byte, error) {
	return redactedJSON("Record")
}

func (RequestTarget) String() string                { return redactedString("RequestTarget") }
func (RequestTarget) GoString() string              { return redactedString("RequestTarget") }
func (RequestTarget) MarshalJSON() ([]byte, error)  { return redactedJSON("RequestTarget") }
func (LeaseIdentity) String() string                { return redactedString("LeaseIdentity") }
func (LeaseIdentity) GoString() string              { return redactedString("LeaseIdentity") }
func (LeaseIdentity) MarshalJSON() ([]byte, error)  { return redactedJSON("LeaseIdentity") }
func (PolicyDecision) String() string               { return redactedString("PolicyDecision") }
func (PolicyDecision) GoString() string             { return redactedString("PolicyDecision") }
func (PolicyDecision) MarshalJSON() ([]byte, error) { return redactedJSON("PolicyDecision") }
func (PolicyDecisionInput) String() string          { return redactedString("PolicyDecisionInput") }
func (PolicyDecisionInput) GoString() string        { return redactedString("PolicyDecisionInput") }
func (PolicyDecisionInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("PolicyDecisionInput")
}
func (PolicyGroup) String() string               { return redactedString("PolicyGroup") }
func (PolicyGroup) GoString() string             { return redactedString("PolicyGroup") }
func (PolicyGroup) MarshalJSON() ([]byte, error) { return redactedJSON("PolicyGroup") }
func (ReservationIntent) String() string         { return redactedString("ReservationIntent") }
func (ReservationIntent) GoString() string       { return redactedString("ReservationIntent") }
func (ReservationIntent) MarshalJSON() ([]byte, error) {
	return redactedJSON("ReservationIntent")
}
func (PublicationIdentity) String() string   { return redactedString("PublicationIdentity") }
func (PublicationIdentity) GoString() string { return redactedString("PublicationIdentity") }
func (PublicationIdentity) MarshalJSON() ([]byte, error) {
	return redactedJSON("PublicationIdentity")
}
func (CommitIdentity) String() string               { return redactedString("CommitIdentity") }
func (CommitIdentity) GoString() string             { return redactedString("CommitIdentity") }
func (CommitIdentity) MarshalJSON() ([]byte, error) { return redactedJSON("CommitIdentity") }

func (SourceJob) String() string               { return redactedString("SourceJob") }
func (SourceJob) GoString() string             { return redactedString("SourceJob") }
func (SourceJob) MarshalJSON() ([]byte, error) { return redactedJSON("SourceJob") }
func (OutputPage) String() string              { return redactedString("OutputPage") }
func (OutputPage) GoString() string            { return redactedString("OutputPage") }
func (OutputPage) MarshalJSON() ([]byte, error) {
	return redactedJSON("OutputPage")
}
func (OutputImage) String() string               { return redactedString("OutputImage") }
func (OutputImage) GoString() string             { return redactedString("OutputImage") }
func (OutputImage) MarshalJSON() ([]byte, error) { return redactedJSON("OutputImage") }
func (OutputDiscovery) String() string           { return redactedString("OutputDiscovery") }
func (OutputDiscovery) GoString() string         { return redactedString("OutputDiscovery") }
func (OutputDiscovery) MarshalJSON() ([]byte, error) {
	return redactedJSON("OutputDiscovery")
}
func (outputAlias) String() string               { return redactedString("outputAlias") }
func (outputAlias) GoString() string             { return redactedString("outputAlias") }
func (outputAlias) MarshalJSON() ([]byte, error) { return redactedJSON("outputAlias") }
func (SuccessfulDocumentRequest) String() string {
	return redactedString("SuccessfulDocumentRequest")
}
func (SuccessfulDocumentRequest) GoString() string {
	return redactedString("SuccessfulDocumentRequest")
}
func (SuccessfulDocumentRequest) MarshalJSON() ([]byte, error) {
	return redactedJSON("SuccessfulDocumentRequest")
}
func (OutputContext) String() string               { return redactedString("OutputContext") }
func (OutputContext) GoString() string             { return redactedString("OutputContext") }
func (OutputContext) MarshalJSON() ([]byte, error) { return redactedJSON("OutputContext") }
func (CrawlOutput) String() string                 { return redactedString("CrawlOutput") }
func (CrawlOutput) GoString() string               { return redactedString("CrawlOutput") }
func (CrawlOutput) MarshalJSON() ([]byte, error)   { return redactedJSON("CrawlOutput") }
func (StageChunk) String() string                  { return redactedString("StageChunk") }
func (StageChunk) GoString() string                { return redactedString("StageChunk") }
func (StageChunk) MarshalJSON() ([]byte, error)    { return redactedJSON("StageChunk") }

func (RejectReadyTransitionInput) String() string {
	return redactedString("RejectReadyTransitionInput")
}
func (RejectReadyTransitionInput) GoString() string {
	return redactedString("RejectReadyTransitionInput")
}
func (RejectReadyTransitionInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("RejectReadyTransitionInput")
}
func (TryClaimTransitionInput) String() string {
	return redactedString("TryClaimTransitionInput")
}
func (TryClaimTransitionInput) GoString() string {
	return redactedString("TryClaimTransitionInput")
}
func (TryClaimTransitionInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("TryClaimTransitionInput")
}
func (ReleaseBeforeIOTransitionInput) String() string {
	return redactedString("ReleaseBeforeIOTransitionInput")
}
func (ReleaseBeforeIOTransitionInput) GoString() string {
	return redactedString("ReleaseBeforeIOTransitionInput")
}
func (ReleaseBeforeIOTransitionInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("ReleaseBeforeIOTransitionInput")
}
func (RetryTransitionInput) String() string { return redactedString("RetryTransitionInput") }
func (RetryTransitionInput) GoString() string {
	return redactedString("RetryTransitionInput")
}
func (RetryTransitionInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("RetryTransitionInput")
}
func (DeadTransitionInput) String() string { return redactedString("DeadTransitionInput") }
func (DeadTransitionInput) GoString() string {
	return redactedString("DeadTransitionInput")
}
func (DeadTransitionInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("DeadTransitionInput")
}
func (CancelJobTransitionInput) String() string {
	return redactedString("CancelJobTransitionInput")
}
func (CancelJobTransitionInput) GoString() string {
	return redactedString("CancelJobTransitionInput")
}
func (CancelJobTransitionInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("CancelJobTransitionInput")
}
func (CompleteNoOutputTransitionInput) String() string {
	return redactedString("CompleteNoOutputTransitionInput")
}
func (CompleteNoOutputTransitionInput) GoString() string {
	return redactedString("CompleteNoOutputTransitionInput")
}
func (CompleteNoOutputTransitionInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("CompleteNoOutputTransitionInput")
}
func (AbortStageTransitionInput) String() string {
	return redactedString("AbortStageTransitionInput")
}
func (AbortStageTransitionInput) GoString() string {
	return redactedString("AbortStageTransitionInput")
}
func (AbortStageTransitionInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("AbortStageTransitionInput")
}

func (StartRequestStarted) String() string { return redactedString("StartRequestStarted") }
func (StartRequestStarted) GoString() string {
	return redactedString("StartRequestStarted")
}
func (StartRequestStarted) MarshalJSON() ([]byte, error) {
	return redactedJSON("StartRequestStarted")
}
func (StartRequestRateBlocked) String() string {
	return redactedString("StartRequestRateBlocked")
}
func (StartRequestRateBlocked) GoString() string {
	return redactedString("StartRequestRateBlocked")
}
func (StartRequestRateBlocked) MarshalJSON() ([]byte, error) {
	return redactedJSON("StartRequestRateBlocked")
}
func (LeaseLostResponse) String() string { return redactedString("LeaseLostResponse") }
func (LeaseLostResponse) GoString() string {
	return redactedString("LeaseLostResponse")
}
func (LeaseLostResponse) MarshalJSON() ([]byte, error) {
	return redactedJSON("LeaseLostResponse")
}
func (StartRequestResponse) String() string { return redactedString("StartRequestResponse") }
func (StartRequestResponse) GoString() string {
	return redactedString("StartRequestResponse")
}
func (StartRequestResponse) MarshalJSON() ([]byte, error) {
	return redactedJSON("StartRequestResponse")
}

func (parsedResponse) String() string               { return redactedString("parsedResponse") }
func (parsedResponse) GoString() string             { return redactedString("parsedResponse") }
func (parsedResponse) MarshalJSON() ([]byte, error) { return redactedJSON("parsedResponse") }
