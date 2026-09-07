package crawljobsv2

import (
	"fmt"
	"log/slog"
	"strconv"
)

// Formatting and text marshaling are intentionally lossy. They are safety
// surfaces for diagnostics and generic log adapters, not protocol codecs.
func formatRedacted(state fmt.State, verb rune, rendered string) {
	if verb == 'q' {
		rendered = strconv.Quote(rendered)
	}
	_, _ = state.Write([]byte(rendered))
}

func redactedPrimitiveText() ([]byte, error) {
	return []byte(redactedValue), nil
}

func redactedCompositeText(typeName string) ([]byte, error) {
	return []byte(redactedString(typeName)), nil
}

func redactedLogValue(typeName string) slog.Value {
	return slog.GroupValue(
		slog.String("type", "crawljobsv2."+typeName),
		slog.Bool("redacted", true),
	)
}

func (RunID) Format(state fmt.State, verb rune)      { formatRedacted(state, verb, redactedValue) }
func (RunID) MarshalText() ([]byte, error)           { return redactedPrimitiveText() }
func (RunID) LogValue() slog.Value                   { return redactedLogValue("RunID") }
func (JobID) Format(state fmt.State, verb rune)      { formatRedacted(state, verb, redactedValue) }
func (JobID) MarshalText() ([]byte, error)           { return redactedPrimitiveText() }
func (JobID) LogValue() slog.Value                   { return redactedLogValue("JobID") }
func (OwnerID) Format(state fmt.State, verb rune)    { formatRedacted(state, verb, redactedValue) }
func (OwnerID) MarshalText() ([]byte, error)         { return redactedPrimitiveText() }
func (OwnerID) LogValue() slog.Value                 { return redactedLogValue("OwnerID") }
func (LeaseToken) Format(state fmt.State, verb rune) { formatRedacted(state, verb, redactedValue) }
func (LeaseToken) MarshalText() ([]byte, error)      { return redactedPrimitiveText() }
func (LeaseToken) LogValue() slog.Value              { return redactedLogValue("LeaseToken") }
func (Digest) Format(state fmt.State, verb rune)     { formatRedacted(state, verb, redactedValue) }
func (Digest) MarshalText() ([]byte, error)          { return redactedPrimitiveText() }
func (Digest) LogValue() slog.Value                  { return redactedLogValue("Digest") }
func (ReservationID) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedValue)
}
func (ReservationID) MarshalText() ([]byte, error) { return redactedPrimitiveText() }
func (ReservationID) LogValue() slog.Value         { return redactedLogValue("ReservationID") }
func (RateScopeID) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedValue)
}
func (RateScopeID) MarshalText() ([]byte, error)  { return redactedPrimitiveText() }
func (RateScopeID) LogValue() slog.Value          { return redactedLogValue("RateScopeID") }
func (GroupID) Format(state fmt.State, verb rune) { formatRedacted(state, verb, redactedValue) }
func (GroupID) MarshalText() ([]byte, error)      { return redactedPrimitiveText() }
func (GroupID) LogValue() slog.Value              { return redactedLogValue("GroupID") }
func (CanonicalOrigin) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedValue)
}
func (CanonicalOrigin) MarshalText() ([]byte, error) { return redactedPrimitiveText() }
func (CanonicalOrigin) LogValue() slog.Value         { return redactedLogValue("CanonicalOrigin") }

func (Field) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("Field"))
}
func (Field) MarshalText() ([]byte, error) { return redactedCompositeText("Field") }
func (Field) LogValue() slog.Value         { return redactedLogValue("Field") }
func (Record) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("Record"))
}
func (Record) MarshalText() ([]byte, error) { return redactedCompositeText("Record") }
func (Record) LogValue() slog.Value         { return redactedLogValue("Record") }
func (RequestTarget) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("RequestTarget"))
}
func (RequestTarget) MarshalText() ([]byte, error) {
	return redactedCompositeText("RequestTarget")
}
func (RequestTarget) LogValue() slog.Value { return redactedLogValue("RequestTarget") }
func (LeaseIdentity) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("LeaseIdentity"))
}
func (LeaseIdentity) MarshalText() ([]byte, error) {
	return redactedCompositeText("LeaseIdentity")
}
func (LeaseIdentity) LogValue() slog.Value { return redactedLogValue("LeaseIdentity") }
func (PolicyDecision) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("PolicyDecision"))
}
func (PolicyDecision) MarshalText() ([]byte, error) {
	return redactedCompositeText("PolicyDecision")
}
func (PolicyDecision) LogValue() slog.Value { return redactedLogValue("PolicyDecision") }
func (PolicyDecisionInput) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("PolicyDecisionInput"))
}
func (PolicyDecisionInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("PolicyDecisionInput")
}
func (PolicyDecisionInput) LogValue() slog.Value {
	return redactedLogValue("PolicyDecisionInput")
}
func (PolicyGroup) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("PolicyGroup"))
}
func (PolicyGroup) MarshalText() ([]byte, error) { return redactedCompositeText("PolicyGroup") }
func (PolicyGroup) LogValue() slog.Value         { return redactedLogValue("PolicyGroup") }
func (ReservationIntent) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("ReservationIntent"))
}
func (ReservationIntent) MarshalText() ([]byte, error) {
	return redactedCompositeText("ReservationIntent")
}
func (ReservationIntent) LogValue() slog.Value { return redactedLogValue("ReservationIntent") }
func (PublicationIdentity) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("PublicationIdentity"))
}
func (PublicationIdentity) MarshalText() ([]byte, error) {
	return redactedCompositeText("PublicationIdentity")
}
func (PublicationIdentity) LogValue() slog.Value {
	return redactedLogValue("PublicationIdentity")
}
func (CommitIdentity) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("CommitIdentity"))
}
func (CommitIdentity) MarshalText() ([]byte, error) {
	return redactedCompositeText("CommitIdentity")
}
func (CommitIdentity) LogValue() slog.Value { return redactedLogValue("CommitIdentity") }

func (SourceJob) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("SourceJob"))
}
func (SourceJob) MarshalText() ([]byte, error) { return redactedCompositeText("SourceJob") }
func (SourceJob) LogValue() slog.Value         { return redactedLogValue("SourceJob") }
func (OutputPage) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("OutputPage"))
}
func (OutputPage) MarshalText() ([]byte, error) { return redactedCompositeText("OutputPage") }
func (OutputPage) LogValue() slog.Value         { return redactedLogValue("OutputPage") }
func (OutputImage) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("OutputImage"))
}
func (OutputImage) MarshalText() ([]byte, error) {
	return redactedCompositeText("OutputImage")
}
func (OutputImage) LogValue() slog.Value { return redactedLogValue("OutputImage") }
func (OutputDiscovery) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("OutputDiscovery"))
}
func (OutputDiscovery) MarshalText() ([]byte, error) {
	return redactedCompositeText("OutputDiscovery")
}
func (OutputDiscovery) LogValue() slog.Value { return redactedLogValue("OutputDiscovery") }
func (outputAlias) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("outputAlias"))
}
func (outputAlias) MarshalText() ([]byte, error) {
	return redactedCompositeText("outputAlias")
}
func (outputAlias) LogValue() slog.Value { return redactedLogValue("outputAlias") }
func (SuccessfulDocumentRequest) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("SuccessfulDocumentRequest"))
}
func (SuccessfulDocumentRequest) MarshalText() ([]byte, error) {
	return redactedCompositeText("SuccessfulDocumentRequest")
}
func (SuccessfulDocumentRequest) LogValue() slog.Value {
	return redactedLogValue("SuccessfulDocumentRequest")
}
func (OutputContext) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("OutputContext"))
}
func (OutputContext) MarshalText() ([]byte, error) {
	return redactedCompositeText("OutputContext")
}
func (OutputContext) LogValue() slog.Value { return redactedLogValue("OutputContext") }
func (CrawlOutput) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("CrawlOutput"))
}
func (CrawlOutput) MarshalText() ([]byte, error) { return redactedCompositeText("CrawlOutput") }
func (CrawlOutput) LogValue() slog.Value         { return redactedLogValue("CrawlOutput") }
func (StageChunk) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("StageChunk"))
}
func (StageChunk) MarshalText() ([]byte, error) { return redactedCompositeText("StageChunk") }
func (StageChunk) LogValue() slog.Value         { return redactedLogValue("StageChunk") }

func (RejectReadyTransitionInput) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("RejectReadyTransitionInput"))
}
func (RejectReadyTransitionInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("RejectReadyTransitionInput")
}
func (RejectReadyTransitionInput) LogValue() slog.Value {
	return redactedLogValue("RejectReadyTransitionInput")
}
func (TryClaimTransitionInput) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("TryClaimTransitionInput"))
}
func (TryClaimTransitionInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("TryClaimTransitionInput")
}
func (TryClaimTransitionInput) LogValue() slog.Value {
	return redactedLogValue("TryClaimTransitionInput")
}
func (ReleaseBeforeIOTransitionInput) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("ReleaseBeforeIOTransitionInput"))
}
func (ReleaseBeforeIOTransitionInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("ReleaseBeforeIOTransitionInput")
}
func (ReleaseBeforeIOTransitionInput) LogValue() slog.Value {
	return redactedLogValue("ReleaseBeforeIOTransitionInput")
}
func (RetryTransitionInput) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("RetryTransitionInput"))
}
func (RetryTransitionInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("RetryTransitionInput")
}
func (RetryTransitionInput) LogValue() slog.Value {
	return redactedLogValue("RetryTransitionInput")
}
func (DeadTransitionInput) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("DeadTransitionInput"))
}
func (DeadTransitionInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("DeadTransitionInput")
}
func (DeadTransitionInput) LogValue() slog.Value {
	return redactedLogValue("DeadTransitionInput")
}
func (CancelJobTransitionInput) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("CancelJobTransitionInput"))
}
func (CancelJobTransitionInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("CancelJobTransitionInput")
}
func (CancelJobTransitionInput) LogValue() slog.Value {
	return redactedLogValue("CancelJobTransitionInput")
}
func (CompleteNoOutputTransitionInput) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("CompleteNoOutputTransitionInput"))
}
func (CompleteNoOutputTransitionInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("CompleteNoOutputTransitionInput")
}
func (CompleteNoOutputTransitionInput) LogValue() slog.Value {
	return redactedLogValue("CompleteNoOutputTransitionInput")
}
func (AbortStageTransitionInput) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("AbortStageTransitionInput"))
}
func (AbortStageTransitionInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("AbortStageTransitionInput")
}
func (AbortStageTransitionInput) LogValue() slog.Value {
	return redactedLogValue("AbortStageTransitionInput")
}

func (StartRequestStarted) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("StartRequestStarted"))
}
func (StartRequestStarted) MarshalText() ([]byte, error) {
	return redactedCompositeText("StartRequestStarted")
}
func (StartRequestStarted) LogValue() slog.Value {
	return redactedLogValue("StartRequestStarted")
}
func (StartRequestRateBlocked) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("StartRequestRateBlocked"))
}
func (StartRequestRateBlocked) MarshalText() ([]byte, error) {
	return redactedCompositeText("StartRequestRateBlocked")
}
func (StartRequestRateBlocked) LogValue() slog.Value {
	return redactedLogValue("StartRequestRateBlocked")
}
func (LeaseLostResponse) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("LeaseLostResponse"))
}
func (LeaseLostResponse) MarshalText() ([]byte, error) {
	return redactedCompositeText("LeaseLostResponse")
}
func (LeaseLostResponse) LogValue() slog.Value { return redactedLogValue("LeaseLostResponse") }
func (StartRequestResponse) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("StartRequestResponse"))
}
func (StartRequestResponse) MarshalText() ([]byte, error) {
	return redactedCompositeText("StartRequestResponse")
}
func (StartRequestResponse) LogValue() slog.Value {
	return redactedLogValue("StartRequestResponse")
}
func (parsedResponse) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("parsedResponse"))
}
func (parsedResponse) MarshalText() ([]byte, error) {
	return redactedCompositeText("parsedResponse")
}
func (parsedResponse) LogValue() slog.Value { return redactedLogValue("parsedResponse") }
