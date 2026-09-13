package crawljobsv2

import (
	"bytes"
	"sort"
	"unicode/utf8"
)

// OperationWireRequest is an opaque member of the closed request family. Its
// operation, gate, KEYS, field ordering, counts, and encoded records are all
// fixed by operation-specific constructors in the operation_wire_* files.
type OperationWireRequest struct {
	operation   OperationName
	gate        TransportGate
	hasGate     bool
	semantic    Record
	records     []Record
	recordBytes [][]byte
	repeated    [][]byte
	keys        [][]byte
	keyContext  operationWireKeyContext
	chunk       operationWireChunkContext
	initialized bool
}

type operationWireKeyContext struct {
	runID         RunID
	jobID         JobID
	commitID      Digest
	reservationID ReservationID
	scopeIDs      []Digest
	activeRunIDs  []RunID
	recordJobIDs  []JobID
	expectedJobID JobID
}

type operationWireChunkContext struct {
	present  bool
	kind     ChunkKind
	ordinal  uint64
	digest   Digest
	commitID Digest
}

func (request OperationWireRequest) Operation() OperationName { return request.operation }

func newOperationWireRequest(
	operation OperationName,
	gate *TransportGate,
	semantic Record,
	records []Record,
	repeated [][]byte,
	context operationWireKeyContext,
	chunk operationWireChunkContext,
) (OperationWireRequest, error) {
	request := OperationWireRequest{
		operation: operation, semantic: cloneRecord(semantic), records: cloneRecords(records),
		repeated: cloneByteSlices(repeated), keyContext: cloneOperationWireKeyContext(context),
		chunk: chunk, initialized: true,
	}
	if gate != nil {
		request.gate = *gate
		request.hasGate = true
	}
	request.recordBytes = make([][]byte, len(request.records))
	for index, record := range request.records {
		encoded, err := EncodeRecord(record)
		if err != nil {
			return OperationWireRequest{}, err
		}
		request.recordBytes[index] = encoded
	}
	keys, err := expectedOperationWireKeys(request)
	if err != nil {
		return OperationWireRequest{}, err
	}
	request.keys = keys
	if _, _, _, err := request.validatedWireParts(); err != nil {
		return OperationWireRequest{}, err
	}
	return request, nil
}

func (request OperationWireRequest) validatedWireParts() ([][]byte, [][]byte, uint64, error) {
	if !request.initialized {
		return nil, nil, 0, ErrInvalidOperationWire
	}
	specification, ok := operationWireSpecifications[request.operation]
	if !ok || specification.operation != request.operation {
		return nil, nil, 0, ErrInvalidOperationWire
	}
	if err := validateOperationWireGate(request, specification); err != nil {
		return nil, nil, 0, err
	}
	if !hasFieldNames(request.semantic, specification.semanticFields...) {
		return nil, nil, 0, ErrOperationWireArguments
	}
	for _, field := range request.semantic {
		if !utf8.Valid(field.Value) {
			return nil, nil, 0, ErrOperationWireArguments
		}
	}

	recordCount, err := validateOperationWireTail(request, specification)
	if err != nil {
		return nil, nil, 0, err
	}
	if err := validateOperationWireChunk(request, specification); err != nil {
		return nil, nil, 0, err
	}
	if err := validateCreateRunWireSource(request); err != nil {
		return nil, nil, 0, err
	}
	if err := validateOperationWireKeyBindings(request); err != nil {
		return nil, nil, 0, err
	}

	expectedKeys, err := expectedOperationWireKeys(request)
	if err != nil {
		return nil, nil, 0, err
	}
	if !equalByteSlices(request.keys, expectedKeys) || !uniqueWireKeys(request.keys) {
		return nil, nil, 0, ErrOperationWireKeys
	}

	arguments := make([][]byte, 0, 7+len(request.semantic)+len(request.repeated)+len(request.recordBytes))
	if request.hasGate {
		gateArguments, err := request.gate.Arguments()
		if err != nil {
			return nil, nil, 0, err
		}
		arguments = append(arguments, gateArguments...)
	}
	for _, field := range request.semantic {
		arguments = append(arguments, append([]byte(nil), field.Value...))
	}
	arguments = append(arguments, cloneByteSlices(request.repeated)...)
	arguments = append(arguments, cloneByteSlices(request.recordBytes)...)

	limit, err := evalSHAOperationLimit(request.operation)
	if err != nil || limit != specification.requestByteLimit {
		return nil, nil, 0, ErrInvalidOperationWire
	}
	return cloneByteSlices(request.keys), arguments, recordCount, nil
}

func validateOperationWireGate(request OperationWireRequest, specification operationWireSpecification) error {
	if len(specification.gateModes) == 0 {
		if request.hasGate || request.operation != OperationApproveBoot {
			return ErrInvalidTransportGate
		}
		return nil
	}
	if !request.hasGate || request.gate.operation != request.operation || request.gate.validate() != nil {
		return ErrInvalidTransportGate
	}
	for _, mode := range specification.gateModes {
		if request.gate.mode == mode {
			return nil
		}
	}
	return ErrInvalidTransportGate
}

func validateOperationWireTail(request OperationWireRequest, specification operationWireSpecification) (uint64, error) {
	switch specification.tailKind {
	case wireTailNone:
		if len(request.records) != 0 || len(request.recordBytes) != 0 || len(request.repeated) != 0 {
			return 0, ErrOperationWireArguments
		}
		return 0, nil
	case wireTailActiveRunIDs:
		if len(request.records) != 0 || len(request.recordBytes) != 0 || len(request.repeated) < int(specification.minimumRecords) ||
			uint64(len(request.repeated)) > specification.maximumRecords {
			return 0, ErrOperationWireArguments
		}
		if err := validateDerivedTailCount(request.semantic, specification.tailCountField, len(request.repeated)); err != nil {
			return 0, err
		}
		if len(request.keyContext.activeRunIDs) != len(request.repeated) {
			return 0, ErrOperationWireArguments
		}
		for index, value := range request.repeated {
			runID, err := ParseRunID(string(value))
			if err != nil || runID != request.keyContext.activeRunIDs[index] || index > 0 && string(value) <= string(request.repeated[index-1]) {
				return 0, ErrOperationWireArguments
			}
		}
		return 0, nil
	case wireTailRecords:
		if len(request.repeated) != 0 || len(request.records) != len(request.recordBytes) ||
			uint64(len(request.records)) < specification.minimumRecords || uint64(len(request.records)) > specification.maximumRecords {
			return 0, ErrOperationWireRecords
		}
		if err := validateDerivedTailCount(request.semantic, specification.tailCountField, len(request.records)); err != nil {
			return 0, err
		}
		for index, record := range request.records {
			if !hasFieldNames(record, specification.recordFields...) {
				return 0, ErrOperationWireRecords
			}
			if index > 0 && operationWireRecordsMustBeSorted(request.operation) &&
				bytes.Compare(record[0].Value, request.records[index-1][0].Value) <= 0 {
				return 0, ErrOperationWireRecords
			}
			encoded, err := EncodeRecord(record)
			if err != nil || !bytes.Equal(encoded, request.recordBytes[index]) {
				return 0, ErrOperationWireRecords
			}
		}
		if err := validateOperationRecordCount(request.operation, uint64(len(request.records))); err != nil {
			return 0, ErrRecordBoundsExceeded
		}
		return uint64(len(request.records)), nil
	default:
		return 0, ErrInvalidOperationWire
	}
}

func validateDerivedTailCount(semantic Record, name string, count int) error {
	for _, field := range semantic {
		if field.Name != name {
			continue
		}
		if string(field.Value) != canonicalDecimal(uint64(count)) {
			return ErrOperationWireRecords
		}
		return nil
	}
	return ErrOperationWireArguments
}

func validateOperationWireChunk(request OperationWireRequest, specification operationWireSpecification) error {
	if len(specification.chunkKinds) == 0 {
		if request.chunk.present {
			return ErrOperationWireChunk
		}
		return nil
	}
	if !request.chunk.present || request.chunk.commitID != request.keyContext.commitID ||
		validateNonzeroDigest(request.chunk.commitID) != nil || validateNonzeroDigest(request.chunk.digest) != nil {
		return ErrOperationWireChunk
	}
	allowed := false
	for _, kind := range specification.chunkKinds {
		if request.chunk.kind == kind {
			allowed = true
			break
		}
	}
	if !allowed {
		return ErrOperationWireChunk
	}
	values := operationWireSemanticValues(request.semantic)
	if string(request.chunk.commitID) != values["commit_id"] || string(request.chunk.kind) != values["chunk_kind"] ||
		canonicalDecimal(request.chunk.ordinal) != values["chunk_ordinal"] || string(request.chunk.digest) != values["chunk_digest"] {
		return ErrOperationWireChunk
	}
	chunk, err := newValidatedStageChunk(request.chunk.commitID, request.chunk.kind, request.chunk.ordinal, request.records)
	if err != nil {
		return ErrOperationWireChunk
	}
	digest, err := DeriveChunkDigest(chunk)
	if err != nil || digest != request.chunk.digest {
		return ErrOperationWireChunk
	}
	return nil
}

func validateCreateRunWireSource(request OperationWireRequest) error {
	if request.operation != OperationCreateRun {
		return nil
	}
	values := operationWireSemanticValues(request.semantic)
	sourceKind, err := ParseSourceKind(values["source_kind"])
	if err != nil {
		return err
	}
	switch request.gate.mode {
	case GateCandidate:
		if sourceKind != SourceV1Migration {
			return ErrInvalidSourceKind
		}
	case GateActive:
		if sourceKind != SourceMongo {
			return ErrInvalidSourceKind
		}
	default:
		return ErrInvalidTransportGate
	}
	return nil
}

func validateOperationWireKeyBindings(request OperationWireRequest) error {
	values := operationWireSemanticValues(request.semantic)
	context := request.keyContext
	if value, present := values["run_id"]; present && value != string(context.runID) {
		return ErrOperationWireKeys
	}
	if value, present := values["candidate_run_id"]; present && value != string(context.runID) {
		return ErrOperationWireKeys
	}
	if value, present := values["job_id"]; present && value != string(context.jobID) {
		return ErrOperationWireKeys
	}
	if value, present := values["commit_id"]; present && value != string(context.commitID) {
		return ErrOperationWireKeys
	}
	if value, present := values["expected_commit_id"]; present && value != string(context.commitID) {
		return ErrOperationWireKeys
	}
	if value, present := values["reservation_id"]; present && value != string(context.reservationID) {
		return ErrOperationWireKeys
	}
	if value, present := values["expected_first_job_id_or_empty"]; present && value != string(context.expectedJobID) {
		return ErrOperationWireKeys
	}
	if len(context.recordJobIDs) != 0 {
		if len(context.recordJobIDs) != len(request.records) {
			return ErrOperationWireKeys
		}
		for index, jobID := range context.recordJobIDs {
			if len(request.records[index]) == 0 || string(request.records[index][0].Value) != string(jobID) {
				return ErrOperationWireKeys
			}
		}
	}
	return nil
}

func operationWireRecordsMustBeSorted(operation OperationName) bool {
	switch operation {
	case OperationCreateRun, OperationEnqueueBatch, OperationAuditRunBatch,
		OperationStageOutlinksBatch, OperationStageDiscoveriesBatch,
		OperationStageAliasesBatch, OperationStageImagesBatch:
		return true
	default:
		return false
	}
}

func operationWireSemanticValues(record Record) map[string]string {
	values := make(map[string]string, len(record))
	for _, field := range record {
		values[field.Name] = string(field.Value)
	}
	return values
}

func expectedOperationWireKeys(request OperationWireRequest) ([][]byte, error) {
	specification, ok := operationWireSpecifications[request.operation]
	if !ok {
		return nil, ErrInvalidOperationWire
	}
	context := request.keyContext
	keys := make([]string, 0, 128)
	if specification.keyPlan == wireKeysApproveBoot {
		return stringsToWireKeys([]string{DurabilityKey}), nil
	}
	keys = append(keys, operationWireAuthorityKeys()...)

	switch specification.keyPlan {
	case wireKeysAuthority:
	case wireKeysRetire:
		keys = append(keys, RunsKey, ActiveRunsKey, UnarchivedRunsKey)
		keys = append(keys, legacyWireKeys()...)
	case wireKeysPromote:
		keys = append(keys,
			RunsKey, ActiveRunsKey, UnarchivedRunsKey, FirstRequestStartKey, ActiveLeasesKey,
			StageExpiryKey, StageSlotsKey, RateScopesKey,
		)
		keys = append(keys, legacyWireKeys()...)
		keys = append(keys, downstreamWireKeys()...)
		if context.runID != "" {
			runKeys, err := operationWireRunKeys(context.runID)
			if err != nil {
				return nil, err
			}
			keys = append(keys, runKeys...)
		}
	case wireKeysMarkShutdown:
		keys = append(keys, ActiveRunsKey, ActiveLeasesKey, StageSlotsKey, StageExpiryKey, RateScopesKey)
		globalScopeKeys, err := operationWireRateScopeKeys(DeriveGlobalScopeID())
		if err != nil {
			return nil, err
		}
		keys = append(keys, globalScopeKeys...)
		keys = append(keys, PagesQueueOwnerKey, ImageIndexerQueueOwnerKey)
		for _, runID := range context.activeRunIDs {
			runKey, err := RunKey(runID)
			if err != nil {
				return nil, ErrOperationWireKeys
			}
			leasedKey, err := RunLeasedKey(runID)
			if err != nil {
				return nil, ErrOperationWireKeys
			}
			keys = append(keys, runKey, leasedKey)
		}
	case wireKeysCreateRun:
		keys = append(keys, RunsKey, ActiveRunsKey, UnarchivedRunsKey)
		runKeys, err := operationWireRunKeys(context.runID)
		if err != nil {
			return nil, err
		}
		keys = append(keys, runKeys...)
	case wireKeysRunRecords:
		keys = append(keys, RunsKey, ActiveRunsKey, UnarchivedRunsKey)
		runKeys, err := operationWireRunKeys(context.runID)
		if err != nil {
			return nil, err
		}
		keys = append(keys, runKeys...)
		for _, jobID := range context.recordJobIDs {
			jobKey, err := RunJobKey(context.runID, jobID)
			if err != nil {
				return nil, ErrOperationWireKeys
			}
			keys = append(keys, jobKey)
		}
	case wireKeysRun:
		keys = append(keys, RunsKey, ActiveRunsKey, UnarchivedRunsKey)
		runKeys, err := operationWireRunKeys(context.runID)
		if err != nil {
			return nil, err
		}
		keys = append(keys, runKeys...)
		if request.operation == OperationActivateRun {
			keys = append(keys, legacyWireKeys()...)
		}
	case wireKeysJob:
		var err error
		keys, err = appendOperationWireRuntimeRun(keys, context.runID, context.jobID)
		if err != nil {
			return nil, err
		}
	case wireKeysReservation:
		var err error
		keys, err = appendOperationWireRuntimeRun(keys, context.runID, context.jobID)
		if err != nil {
			return nil, err
		}
		if context.reservationID != "" {
			reservationKey, err := ReservationKey(context.reservationID)
			if err != nil {
				return nil, ErrOperationWireKeys
			}
			keys = append(keys, reservationKey)
		}
		for _, scopeID := range context.scopeIDs {
			scopeKeys, err := operationWireRateScopeKeys(scopeID)
			if err != nil {
				return nil, err
			}
			keys = append(keys, scopeKeys...)
		}
	case wireKeysStage:
		var err error
		keys, err = appendOperationWireRuntimeRun(keys, context.runID, context.jobID)
		if err != nil {
			return nil, err
		}
		stageKeys, err := operationWireStageKeys(context.commitID)
		if err != nil {
			return nil, err
		}
		keys = append(keys, stageKeys...)
		if request.operation == OperationCommit {
			keys = append(keys, PagesQueueKey)
		}
	case wireKeysRunMaintenance:
		keys = append(keys, RunsKey, ActiveRunsKey, UnarchivedRunsKey, ActiveLeasesKey, StageExpiryKey, StageSlotsKey, RateScopesKey)
		runKeys, err := operationWireRunKeys(context.runID)
		if err != nil {
			return nil, err
		}
		keys = append(keys, runKeys...)
	case wireKeysArchive:
		keys = append(keys, RunsKey, ActiveRunsKey, UnarchivedRunsKey, ActiveLeasesKey, StageExpiryKey, StageSlotsKey)
		runKeys, err := operationWireRunKeys(context.runID)
		if err != nil {
			return nil, err
		}
		keys = append(keys, runKeys...)
		keys = append(keys, downstreamWireKeys()...)
	case wireKeysPurge:
		keys = append(keys, RunsKey, ActiveRunsKey, UnarchivedRunsKey, FirstRequestStartKey, ActiveLeasesKey, StageExpiryKey, StageSlotsKey)
		runKeys, err := operationWireRunKeys(context.runID)
		if err != nil {
			return nil, err
		}
		keys = append(keys, runKeys...)
		if context.expectedJobID != "" {
			jobKey, err := RunJobKey(context.runID, context.expectedJobID)
			if err != nil {
				return nil, ErrOperationWireKeys
			}
			keys = append(keys, jobKey)
		}
	case wireKeysCleanStage:
		keys = append(keys, StageExpiryKey, StageSlotsKey)
		stageKeys, err := operationWireStageKeys(context.commitID)
		if err != nil {
			return nil, err
		}
		keys = append(keys, stageKeys...)
	case wireKeysRateMaintenance:
		keys = append(keys, RateScopesKey)
	default:
		return nil, ErrInvalidOperationWire
	}
	return stringsToWireKeys(keys), nil
}

func operationWireAuthorityKeys() []string {
	return []string{
		DurabilityKey,
		ContractsActiveKey,
		CrawlContractKey,
		ContractsCandidateKey,
		CrawlContractCandidateKey,
		CommitGuardKey,
		LegacyRetirementKey,
		AdminFreezeKey,
	}
}

func legacyWireKeys() []string {
	legacy := LegacyKeysInBitmapOrder()
	return append([]string(nil), legacy[:]...)
}

func downstreamWireKeys() []string {
	queues := DownstreamQueueKeys()
	keys := append([]string(nil), queues[:]...)
	return append(keys, PagesQueueOwnerKey, ImageIndexerQueueOwnerKey)
}

func appendOperationWireRuntimeRun(keys []string, runID RunID, jobID JobID) ([]string, error) {
	keys = append(keys, RunsKey, ActiveRunsKey, UnarchivedRunsKey, FirstRequestStartKey, ActiveLeasesKey, StageExpiryKey, StageSlotsKey, RateScopesKey)
	runKeys, err := operationWireRunKeys(runID)
	if err != nil {
		return nil, err
	}
	keys = append(keys, runKeys...)
	if jobID != "" {
		jobKey, err := RunJobKey(runID, jobID)
		if err != nil {
			return nil, ErrOperationWireKeys
		}
		keys = append(keys, jobKey)
	}
	return keys, nil
}

func operationWireRunKeys(runID RunID) ([]string, error) {
	constructors := []func(RunID) (string, error){
		RunKey,
		RunJobsKey,
		RunJobOrderKey,
		RunReadyKey,
		RunReadyAtKey,
		RunLeasedKey,
		RunLeasedAtKey,
		RunDelayedKey,
		RunCommitBackpressureKey,
		RunCompletedKey,
		RunDeadKey,
		RunCancelledKey,
		RunGroupLimitsKey,
		RunGroupRateScopeIDsKey,
		RunGroupScopeIDsKey,
		RunGroupConcurrencyKey,
		RunGroupIntervalMSKey,
		RunGroupStartedKey,
		RunGroupPendingKey,
		RunGroupActiveStartedKey,
		RunGroupOpenJobsKey,
		RunAuditGroupCountsKey,
		RunRetryReasonCountsKey,
		RunRecoveryOutcomeCountsKey,
		RunDispositionReasonCountsKey,
		RunVisitedDepthKey,
		RunVisitedURLsKey,
	}
	keys := make([]string, len(constructors))
	for index, constructor := range constructors {
		key, err := constructor(runID)
		if err != nil {
			return nil, ErrOperationWireKeys
		}
		keys[index] = key
	}
	return keys, nil
}

func operationWireStageKeys(commitID Digest) ([]string, error) {
	constructors := []func(Digest) (string, error){
		StageMetaKey,
		StageKeysKey,
		StagePageKey,
		StageOutlinksKey,
		StageDiscoveriesKey,
		StageDiscoveryRecordsKey,
		StageDiscoveryDepthsKey,
		StageAliasesKey,
		StageImageManifestKey,
	}
	keys := make([]string, 0, MaxStageKeys)
	for _, constructor := range constructors {
		key, err := constructor(commitID)
		if err != nil {
			return nil, ErrOperationWireKeys
		}
		keys = append(keys, key)
	}
	for index := 0; index < MaxImagesPerPage; index++ {
		key, err := StageImageKey(commitID, index)
		if err != nil {
			return nil, ErrOperationWireKeys
		}
		keys = append(keys, key)
	}
	if len(keys) != MaxStageKeys {
		return nil, ErrOperationWireKeys
	}
	return keys, nil
}

func operationWireRateScopeKeys(scopeID Digest) ([]string, error) {
	base, err := RateScopeKey(scopeID)
	if err != nil {
		return nil, ErrOperationWireKeys
	}
	active, err := RateScopeActiveKey(scopeID)
	if err != nil {
		return nil, ErrOperationWireKeys
	}
	pending, err := RateScopePendingKey(scopeID)
	if err != nil {
		return nil, ErrOperationWireKeys
	}
	started, err := RateScopeStartedKey(scopeID)
	if err != nil {
		return nil, ErrOperationWireKeys
	}
	return []string{base, active, pending, started}, nil
}

func stringsToWireKeys(values []string) [][]byte {
	keys := make([][]byte, len(values))
	for index, value := range values {
		keys[index] = []byte(value)
	}
	return keys
}

func equalByteSlices(left, right [][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !bytes.Equal(left[index], right[index]) {
			return false
		}
	}
	return true
}

func uniqueWireKeys(keys [][]byte) bool {
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if len(key) == 0 || !utf8.Valid(key) {
			return false
		}
		value := string(key)
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func cloneOperationWireKeyContext(context operationWireKeyContext) operationWireKeyContext {
	cloned := context
	cloned.scopeIDs = append([]Digest(nil), context.scopeIDs...)
	cloned.activeRunIDs = append([]RunID(nil), context.activeRunIDs...)
	cloned.recordJobIDs = append([]JobID(nil), context.recordJobIDs...)
	return cloned
}

func sortedUniqueRunIDs(values []RunID) ([]RunID, error) {
	result := append([]RunID(nil), values...)
	for _, runID := range result {
		if err := validateRunID(runID); err != nil {
			return nil, err
		}
	}
	sort.Slice(result, func(left, right int) bool { return string(result[left]) < string(result[right]) })
	for index := 1; index < len(result); index++ {
		if result[index] == result[index-1] {
			return nil, ErrOperationWireArguments
		}
	}
	return result, nil
}

func operationWireGate(operation OperationName, gate TransportGate) (*TransportGate, error) {
	if gate.operation != operation || gate.validate() != nil {
		return nil, ErrInvalidTransportGate
	}
	return &gate, nil
}

func operationWireFields(fields ...Field) Record { return Record(fields) }

func validateWirePositive(value uint64) error {
	if value == 0 || value > MaxExactInteger {
		return ErrInvalidUnsignedDecimal
	}
	return nil
}

func validateWireNonnegative(value uint64) error {
	return validateNonnegativeExactInteger(value)
}
