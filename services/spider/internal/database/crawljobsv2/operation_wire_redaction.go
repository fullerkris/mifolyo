package crawljobsv2

import (
	"fmt"
	"log/slog"
)

func operationWireRedactedFormat(state fmt.State, verb rune, typeName string) {
	formatRedacted(state, verb, redactedString(typeName))
}

func (ScriptBindingReview) String() string { return redactedString("ScriptBindingReview") }
func (ScriptBindingReview) GoString() string {
	return redactedString("ScriptBindingReview")
}
func (ScriptBindingReview) MarshalJSON() ([]byte, error) {
	return redactedJSON("ScriptBindingReview")
}
func (ScriptBindingReview) Format(state fmt.State, verb rune) {
	operationWireRedactedFormat(state, verb, "ScriptBindingReview")
}
func (ScriptBindingReview) MarshalText() ([]byte, error) {
	return redactedCompositeText("ScriptBindingReview")
}
func (ScriptBindingReview) LogValue() slog.Value {
	return redactedLogValue("ScriptBindingReview")
}

func (ScriptBindingSet) String() string   { return redactedString("ScriptBindingSet") }
func (ScriptBindingSet) GoString() string { return redactedString("ScriptBindingSet") }
func (ScriptBindingSet) MarshalJSON() ([]byte, error) {
	return redactedJSON("ScriptBindingSet")
}
func (ScriptBindingSet) Format(state fmt.State, verb rune) {
	operationWireRedactedFormat(state, verb, "ScriptBindingSet")
}
func (ScriptBindingSet) MarshalText() ([]byte, error) {
	return redactedCompositeText("ScriptBindingSet")
}
func (ScriptBindingSet) LogValue() slog.Value { return redactedLogValue("ScriptBindingSet") }

func (ApproveBootWireInput) String() string { return redactedString("ApproveBootWireInput") }
func (ApproveBootWireInput) GoString() string {
	return redactedString("ApproveBootWireInput")
}
func (ApproveBootWireInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("ApproveBootWireInput")
}
func (ApproveBootWireInput) Format(state fmt.State, verb rune) {
	operationWireRedactedFormat(state, verb, "ApproveBootWireInput")
}
func (ApproveBootWireInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("ApproveBootWireInput")
}
func (ApproveBootWireInput) LogValue() slog.Value {
	return redactedLogValue("ApproveBootWireInput")
}

func (InstallCandidateMarkersWireInput) String() string {
	return redactedString("InstallCandidateMarkersWireInput")
}
func (InstallCandidateMarkersWireInput) GoString() string {
	return redactedString("InstallCandidateMarkersWireInput")
}
func (InstallCandidateMarkersWireInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("InstallCandidateMarkersWireInput")
}
func (InstallCandidateMarkersWireInput) Format(state fmt.State, verb rune) {
	operationWireRedactedFormat(state, verb, "InstallCandidateMarkersWireInput")
}
func (InstallCandidateMarkersWireInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("InstallCandidateMarkersWireInput")
}
func (InstallCandidateMarkersWireInput) LogValue() slog.Value {
	return redactedLogValue("InstallCandidateMarkersWireInput")
}

func (RetireLegacyKeysWireInput) String() string {
	return redactedString("RetireLegacyKeysWireInput")
}
func (RetireLegacyKeysWireInput) GoString() string {
	return redactedString("RetireLegacyKeysWireInput")
}
func (RetireLegacyKeysWireInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("RetireLegacyKeysWireInput")
}
func (RetireLegacyKeysWireInput) Format(state fmt.State, verb rune) {
	operationWireRedactedFormat(state, verb, "RetireLegacyKeysWireInput")
}
func (RetireLegacyKeysWireInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("RetireLegacyKeysWireInput")
}
func (RetireLegacyKeysWireInput) LogValue() slog.Value {
	return redactedLogValue("RetireLegacyKeysWireInput")
}

func (PromoteCandidateContractsWireInput) String() string {
	return redactedString("PromoteCandidateContractsWireInput")
}
func (PromoteCandidateContractsWireInput) GoString() string {
	return redactedString("PromoteCandidateContractsWireInput")
}
func (PromoteCandidateContractsWireInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("PromoteCandidateContractsWireInput")
}
func (PromoteCandidateContractsWireInput) Format(state fmt.State, verb rune) {
	operationWireRedactedFormat(state, verb, "PromoteCandidateContractsWireInput")
}
func (PromoteCandidateContractsWireInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("PromoteCandidateContractsWireInput")
}
func (PromoteCandidateContractsWireInput) LogValue() slog.Value {
	return redactedLogValue("PromoteCandidateContractsWireInput")
}

func (MarkPlannedShutdownWireInput) String() string {
	return redactedString("MarkPlannedShutdownWireInput")
}
func (MarkPlannedShutdownWireInput) GoString() string {
	return redactedString("MarkPlannedShutdownWireInput")
}
func (MarkPlannedShutdownWireInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("MarkPlannedShutdownWireInput")
}
func (MarkPlannedShutdownWireInput) Format(state fmt.State, verb rune) {
	operationWireRedactedFormat(state, verb, "MarkPlannedShutdownWireInput")
}
func (MarkPlannedShutdownWireInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("MarkPlannedShutdownWireInput")
}
func (MarkPlannedShutdownWireInput) LogValue() slog.Value {
	return redactedLogValue("MarkPlannedShutdownWireInput")
}

func (CreateRunWireInput) String() string   { return redactedString("CreateRunWireInput") }
func (CreateRunWireInput) GoString() string { return redactedString("CreateRunWireInput") }
func (CreateRunWireInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("CreateRunWireInput")
}
func (CreateRunWireInput) Format(state fmt.State, verb rune) {
	operationWireRedactedFormat(state, verb, "CreateRunWireInput")
}
func (CreateRunWireInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("CreateRunWireInput")
}
func (CreateRunWireInput) LogValue() slog.Value {
	return redactedLogValue("CreateRunWireInput")
}

func (AuditRunBatchWireInput) String() string {
	return redactedString("AuditRunBatchWireInput")
}
func (AuditRunBatchWireInput) GoString() string {
	return redactedString("AuditRunBatchWireInput")
}
func (AuditRunBatchWireInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("AuditRunBatchWireInput")
}
func (AuditRunBatchWireInput) Format(state fmt.State, verb rune) {
	operationWireRedactedFormat(state, verb, "AuditRunBatchWireInput")
}
func (AuditRunBatchWireInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("AuditRunBatchWireInput")
}
func (AuditRunBatchWireInput) LogValue() slog.Value {
	return redactedLogValue("AuditRunBatchWireInput")
}

func (SealRunWireInput) String() string   { return redactedString("SealRunWireInput") }
func (SealRunWireInput) GoString() string { return redactedString("SealRunWireInput") }
func (SealRunWireInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("SealRunWireInput")
}
func (SealRunWireInput) Format(state fmt.State, verb rune) {
	operationWireRedactedFormat(state, verb, "SealRunWireInput")
}
func (SealRunWireInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("SealRunWireInput")
}
func (SealRunWireInput) LogValue() slog.Value { return redactedLogValue("SealRunWireInput") }

func (ActivateRunWireInput) String() string   { return redactedString("ActivateRunWireInput") }
func (ActivateRunWireInput) GoString() string { return redactedString("ActivateRunWireInput") }
func (ActivateRunWireInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("ActivateRunWireInput")
}
func (ActivateRunWireInput) Format(state fmt.State, verb rune) {
	operationWireRedactedFormat(state, verb, "ActivateRunWireInput")
}
func (ActivateRunWireInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("ActivateRunWireInput")
}
func (ActivateRunWireInput) LogValue() slog.Value {
	return redactedLogValue("ActivateRunWireInput")
}

func (BeginStageWireInput) String() string   { return redactedString("BeginStageWireInput") }
func (BeginStageWireInput) GoString() string { return redactedString("BeginStageWireInput") }
func (BeginStageWireInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("BeginStageWireInput")
}
func (BeginStageWireInput) Format(state fmt.State, verb rune) {
	operationWireRedactedFormat(state, verb, "BeginStageWireInput")
}
func (BeginStageWireInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("BeginStageWireInput")
}
func (BeginStageWireInput) LogValue() slog.Value { return redactedLogValue("BeginStageWireInput") }

func (SealStageWireInput) String() string   { return redactedString("SealStageWireInput") }
func (SealStageWireInput) GoString() string { return redactedString("SealStageWireInput") }
func (SealStageWireInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("SealStageWireInput")
}
func (SealStageWireInput) Format(state fmt.State, verb rune) {
	operationWireRedactedFormat(state, verb, "SealStageWireInput")
}
func (SealStageWireInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("SealStageWireInput")
}
func (SealStageWireInput) LogValue() slog.Value { return redactedLogValue("SealStageWireInput") }

func (CancelRunWireInput) String() string   { return redactedString("CancelRunWireInput") }
func (CancelRunWireInput) GoString() string { return redactedString("CancelRunWireInput") }
func (CancelRunWireInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("CancelRunWireInput")
}
func (CancelRunWireInput) Format(state fmt.State, verb rune) {
	operationWireRedactedFormat(state, verb, "CancelRunWireInput")
}
func (CancelRunWireInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("CancelRunWireInput")
}
func (CancelRunWireInput) LogValue() slog.Value { return redactedLogValue("CancelRunWireInput") }

func (ArchiveRunWireInput) String() string   { return redactedString("ArchiveRunWireInput") }
func (ArchiveRunWireInput) GoString() string { return redactedString("ArchiveRunWireInput") }
func (ArchiveRunWireInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("ArchiveRunWireInput")
}
func (ArchiveRunWireInput) Format(state fmt.State, verb rune) {
	operationWireRedactedFormat(state, verb, "ArchiveRunWireInput")
}
func (ArchiveRunWireInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("ArchiveRunWireInput")
}
func (ArchiveRunWireInput) LogValue() slog.Value { return redactedLogValue("ArchiveRunWireInput") }

func (PurgeRunBatchWireInput) String() string {
	return redactedString("PurgeRunBatchWireInput")
}
func (PurgeRunBatchWireInput) GoString() string {
	return redactedString("PurgeRunBatchWireInput")
}
func (PurgeRunBatchWireInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("PurgeRunBatchWireInput")
}
func (PurgeRunBatchWireInput) Format(state fmt.State, verb rune) {
	operationWireRedactedFormat(state, verb, "PurgeRunBatchWireInput")
}
func (PurgeRunBatchWireInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("PurgeRunBatchWireInput")
}
func (PurgeRunBatchWireInput) LogValue() slog.Value {
	return redactedLogValue("PurgeRunBatchWireInput")
}

func (CleanStageWireInput) String() string   { return redactedString("CleanStageWireInput") }
func (CleanStageWireInput) GoString() string { return redactedString("CleanStageWireInput") }
func (CleanStageWireInput) MarshalJSON() ([]byte, error) {
	return redactedJSON("CleanStageWireInput")
}
func (CleanStageWireInput) Format(state fmt.State, verb rune) {
	operationWireRedactedFormat(state, verb, "CleanStageWireInput")
}
func (CleanStageWireInput) MarshalText() ([]byte, error) {
	return redactedCompositeText("CleanStageWireInput")
}
func (CleanStageWireInput) LogValue() slog.Value { return redactedLogValue("CleanStageWireInput") }
