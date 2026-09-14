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
func (ImageDigest) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedValue)
}
func (ImageDigest) MarshalText() ([]byte, error) { return redactedPrimitiveText() }
func (ImageDigest) LogValue() slog.Value         { return redactedLogValue("ImageDigest") }

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
func (DocumentTranscript) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("DocumentTranscript"))
}
func (DocumentTranscript) MarshalText() ([]byte, error) {
	return redactedCompositeText("DocumentTranscript")
}
func (DocumentTranscript) LogValue() slog.Value {
	return redactedLogValue("DocumentTranscript")
}
func (FinalDocumentWitness) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("FinalDocumentWitness"))
}
func (FinalDocumentWitness) MarshalText() ([]byte, error) {
	return redactedCompositeText("FinalDocumentWitness")
}
func (FinalDocumentWitness) LogValue() slog.Value {
	return redactedLogValue("FinalDocumentWitness")
}
func (RenderPolicyAuthorization) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("RenderPolicyAuthorization"))
}
func (RenderPolicyAuthorization) MarshalText() ([]byte, error) {
	return redactedCompositeText("RenderPolicyAuthorization")
}
func (RenderPolicyAuthorization) LogValue() slog.Value {
	return redactedLogValue("RenderPolicyAuthorization")
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
func (RequestIOPermit) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("RequestIOPermit"))
}
func (RequestIOPermit) MarshalText() ([]byte, error) {
	return redactedCompositeText("RequestIOPermit")
}
func (RequestIOPermit) LogValue() slog.Value { return redactedLogValue("RequestIOPermit") }
func (parsedResponse) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("parsedResponse"))
}
func (parsedResponse) MarshalText() ([]byte, error) {
	return redactedCompositeText("parsedResponse")
}
func (parsedResponse) LogValue() slog.Value { return redactedLogValue("parsedResponse") }

func (transportAuthority) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("transportAuthority"))
}
func (transportAuthority) MarshalText() ([]byte, error) {
	return redactedCompositeText("transportAuthority")
}
func (transportAuthority) LogValue() slog.Value {
	return redactedLogValue("transportAuthority")
}
func (startRequestBinding) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("startRequestBinding"))
}
func (startRequestBinding) MarshalText() ([]byte, error) {
	return redactedCompositeText("startRequestBinding")
}
func (startRequestBinding) LogValue() slog.Value {
	return redactedLogValue("startRequestBinding")
}
func (requestIOAuthorityState) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("requestIOAuthorityState"))
}
func (requestIOAuthorityState) MarshalText() ([]byte, error) {
	return redactedCompositeText("requestIOAuthorityState")
}
func (requestIOAuthorityState) LogValue() slog.Value {
	return redactedLogValue("requestIOAuthorityState")
}

func (TransportGateInput) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("TransportGateInput"))
}
func (TransportGateInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("TransportGateInput")
}
func (TransportGateInput) LogValue() slog.Value {
	return redactedLogValue("TransportGateInput")
}
func (TransportGate) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("TransportGate"))
}
func (TransportGate) MarshalText() ([]byte, error) {
	return redactedCompositeText("TransportGate")
}
func (TransportGate) LogValue() slog.Value { return redactedLogValue("TransportGate") }
func (OperationWireRequest) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("OperationWireRequest"))
}
func (OperationWireRequest) MarshalText() ([]byte, error) {
	return redactedCompositeText("OperationWireRequest")
}
func (OperationWireRequest) LogValue() slog.Value {
	return redactedLogValue("OperationWireRequest")
}
func (EvalSHARequest) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("EvalSHARequest"))
}
func (EvalSHARequest) MarshalText() ([]byte, error) {
	return redactedCompositeText("EvalSHARequest")
}
func (EvalSHARequest) LogValue() slog.Value { return redactedLogValue("EvalSHARequest") }

func (CompatibilityArtifactInput) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("CompatibilityArtifactInput"))
}
func (CompatibilityArtifactInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("CompatibilityArtifactInput")
}
func (CompatibilityArtifactInput) LogValue() slog.Value {
	return redactedLogValue("CompatibilityArtifactInput")
}
func (CompatibilityArtifact) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("CompatibilityArtifact"))
}
func (CompatibilityArtifact) MarshalText() ([]byte, error) {
	return redactedCompositeText("CompatibilityArtifact")
}
func (CompatibilityArtifact) LogValue() slog.Value {
	return redactedLogValue("CompatibilityArtifact")
}
func (CompatibilityMarker) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("CompatibilityMarker"))
}
func (CompatibilityMarker) MarshalText() ([]byte, error) {
	return redactedCompositeText("CompatibilityMarker")
}
func (CompatibilityMarker) LogValue() slog.Value {
	return redactedLogValue("CompatibilityMarker")
}
func (GuardCoreInput) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("GuardCoreInput"))
}
func (GuardCoreInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("GuardCoreInput")
}
func (GuardCoreInput) LogValue() slog.Value { return redactedLogValue("GuardCoreInput") }
func (GuardCore) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("GuardCore"))
}
func (GuardCore) MarshalText() ([]byte, error) { return redactedCompositeText("GuardCore") }
func (GuardCore) LogValue() slog.Value         { return redactedLogValue("GuardCore") }
func (ProvisionalGuardCore) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("ProvisionalGuardCore"))
}
func (ProvisionalGuardCore) MarshalText() ([]byte, error) {
	return redactedCompositeText("ProvisionalGuardCore")
}
func (ProvisionalGuardCore) LogValue() slog.Value {
	return redactedLogValue("ProvisionalGuardCore")
}
func (StoredCommitGuard) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("StoredCommitGuard"))
}
func (StoredCommitGuard) MarshalText() ([]byte, error) {
	return redactedCompositeText("StoredCommitGuard")
}
func (StoredCommitGuard) LogValue() slog.Value {
	return redactedLogValue("StoredCommitGuard")
}
func (LegacyRetirementRecordInput) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("LegacyRetirementRecordInput"))
}
func (LegacyRetirementRecordInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("LegacyRetirementRecordInput")
}
func (LegacyRetirementRecordInput) LogValue() slog.Value {
	return redactedLogValue("LegacyRetirementRecordInput")
}
func (LegacyRetirementRecord) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("LegacyRetirementRecord"))
}
func (LegacyRetirementRecord) MarshalText() ([]byte, error) {
	return redactedCompositeText("LegacyRetirementRecord")
}
func (LegacyRetirementRecord) LogValue() slog.Value {
	return redactedLogValue("LegacyRetirementRecord")
}
func (AdminFreezeRecordInput) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("AdminFreezeRecordInput"))
}
func (AdminFreezeRecordInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("AdminFreezeRecordInput")
}
func (AdminFreezeRecordInput) LogValue() slog.Value {
	return redactedLogValue("AdminFreezeRecordInput")
}
func (AdminFreezeRecord) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("AdminFreezeRecord"))
}
func (AdminFreezeRecord) MarshalText() ([]byte, error) {
	return redactedCompositeText("AdminFreezeRecord")
}
func (AdminFreezeRecord) LogValue() slog.Value {
	return redactedLogValue("AdminFreezeRecord")
}
func (DurabilityRecordInput) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("DurabilityRecordInput"))
}
func (DurabilityRecordInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("DurabilityRecordInput")
}
func (DurabilityRecordInput) LogValue() slog.Value {
	return redactedLogValue("DurabilityRecordInput")
}
func (DurabilityRecord) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("DurabilityRecord"))
}
func (DurabilityRecord) MarshalText() ([]byte, error) {
	return redactedCompositeText("DurabilityRecord")
}
func (DurabilityRecord) LogValue() slog.Value { return redactedLogValue("DurabilityRecord") }
func (FirstRequestStartEvidence) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("FirstRequestStartEvidence"))
}
func (FirstRequestStartEvidence) MarshalText() ([]byte, error) {
	return redactedCompositeText("FirstRequestStartEvidence")
}
func (FirstRequestStartEvidence) LogValue() slog.Value {
	return redactedLogValue("FirstRequestStartEvidence")
}
func (FinalPageRecord) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("FinalPageRecord"))
}
func (FinalPageRecord) MarshalText() ([]byte, error) {
	return redactedCompositeText("FinalPageRecord")
}
func (FinalPageRecord) LogValue() slog.Value { return redactedLogValue("FinalPageRecord") }
func (FinalImageRecord) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("FinalImageRecord"))
}
func (FinalImageRecord) MarshalText() ([]byte, error) {
	return redactedCompositeText("FinalImageRecord")
}
func (FinalImageRecord) LogValue() slog.Value { return redactedLogValue("FinalImageRecord") }
func (ImageManifestRecord) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("ImageManifestRecord"))
}
func (ImageManifestRecord) MarshalText() ([]byte, error) {
	return redactedCompositeText("ImageManifestRecord")
}
func (ImageManifestRecord) LogValue() slog.Value {
	return redactedLogValue("ImageManifestRecord")
}
