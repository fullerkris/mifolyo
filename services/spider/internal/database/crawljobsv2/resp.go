package crawljobsv2

import (
	"errors"
	"strconv"
)

var (
	ErrInvalidScriptSHA1     = errors.New("crawljobsv2: invalid script SHA-1")
	ErrCommandBoundsExceeded = errors.New("crawljobsv2: serialized command bounds exceeded")
	ErrRecordBoundsExceeded  = errors.New("crawljobsv2: operation record bounds exceeded")
)

// EvalSHARequest is a validated immutable wire request ready for a Redis
// client. Its script identity comes from a complete sealed authoritative bundle
// rather than from a per-request caller-selected SHA-1.
type EvalSHARequest struct {
	operation    OperationName
	scriptName   string
	scriptSHA1   string
	sourceSHA256 Digest
	bundleSeal   scriptBindingSetSeal
	keys         [][]byte
	arguments    [][]byte
	size         uint64
}

func BuildEvalSHARequest(bundle ScriptBindingSet, request OperationWireRequest) (EvalSHARequest, error) {
	// Validate the trust anchor before processing caller-held request data. The
	// zero value therefore cannot fall through to a different construction path.
	if err := bundle.validate(); err != nil {
		return EvalSHARequest{}, err
	}
	keys, arguments, recordCount, err := request.validatedWireParts()
	if err != nil {
		return EvalSHARequest{}, err
	}
	binding, err := bundle.bindingForValidated(request.operation)
	if err != nil {
		return EvalSHARequest{}, err
	}
	if err := bundle.validateRequestContract(request); err != nil {
		return EvalSHARequest{}, err
	}
	size, err := validateEvalSHARequest(request.operation, binding.redisSHA1, keys, arguments, recordCount)
	if err != nil {
		return EvalSHARequest{}, err
	}
	return EvalSHARequest{
		operation: request.operation, scriptName: binding.sourceName,
		scriptSHA1: binding.redisSHA1, sourceSHA256: binding.sourceSHA256, bundleSeal: *bundle.seal,
		keys: cloneByteSlices(keys), arguments: cloneByteSlices(arguments), size: size,
	}, nil
}

func (request EvalSHARequest) Operation() OperationName { return request.operation }
func (request EvalSHARequest) ScriptName() string       { return request.scriptName }
func (request EvalSHARequest) ScriptSHA1() string       { return request.scriptSHA1 }
func (request EvalSHARequest) SourceSHA256() Digest     { return request.sourceSHA256 }
func (request EvalSHARequest) SerializedSize() uint64   { return request.size }
func (request EvalSHARequest) Keys() [][]byte           { return cloneByteSlices(request.keys) }
func (request EvalSHARequest) Arguments() [][]byte      { return cloneByteSlices(request.arguments) }

func EvalSHASerializedSize(scriptSHA1 string, keys, arguments [][]byte) (uint64, error) {
	if !isLowerHex(scriptSHA1, 40) {
		return 0, ErrInvalidScriptSHA1
	}
	partCount, ok := addSize(3, uint64(len(keys)))
	if !ok {
		return 0, ErrCommandBoundsExceeded
	}
	partCount, ok = addSize(partCount, uint64(len(arguments)))
	if !ok {
		return 0, ErrCommandBoundsExceeded
	}
	size := respArrayHeaderSize(partCount)
	if size, ok = addRESPBulkSize(size, uint64(len("EVALSHA"))); !ok {
		return 0, ErrCommandBoundsExceeded
	}
	if size, ok = addRESPBulkSize(size, uint64(len(scriptSHA1))); !ok {
		return 0, ErrCommandBoundsExceeded
	}
	numKeys := strconv.FormatUint(uint64(len(keys)), 10)
	if size, ok = addRESPBulkSize(size, uint64(len(numKeys))); !ok {
		return 0, ErrCommandBoundsExceeded
	}
	for _, value := range keys {
		if size, ok = addRESPBulkSize(size, uint64(len(value))); !ok {
			return 0, ErrCommandBoundsExceeded
		}
	}
	for _, value := range arguments {
		if size, ok = addRESPBulkSize(size, uint64(len(value))); !ok {
			return 0, ErrCommandBoundsExceeded
		}
	}
	return size, nil
}

func validateEvalSHARequest(operation OperationName, scriptSHA1 string, keys, arguments [][]byte, recordCount uint64) (uint64, error) {
	limit, err := evalSHAOperationLimit(operation)
	if err != nil {
		return 0, err
	}
	if err := validateOperationRecordCount(operation, recordCount); err != nil {
		return 0, ErrRecordBoundsExceeded
	}
	size, err := EvalSHASerializedSize(scriptSHA1, keys, arguments)
	if err != nil {
		return 0, err
	}
	if size > limit {
		return size, ErrCommandBoundsExceeded
	}
	return size, nil
}

func cloneByteSlices(values [][]byte) [][]byte {
	cloned := make([][]byte, len(values))
	for index := range values {
		cloned[index] = append([]byte(nil), values[index]...)
	}
	return cloned
}

func validateOperationRecordCount(operation OperationName, recordCount uint64) error {
	switch operation {
	case OperationEnqueueBatch:
		if recordCount == 0 || recordCount > FeederEnqueueBatchSize {
			return ErrRecordBoundsExceeded
		}
	case OperationAuditRunBatch:
		if recordCount > RunAuditBatchSize {
			return ErrRecordBoundsExceeded
		}
	case OperationStagePageFields, OperationStagePageBlob, OperationStageImageManifest:
		if recordCount != 1 {
			return ErrRecordBoundsExceeded
		}
	case OperationStageOutlinksBatch, OperationStageDiscoveriesBatch:
		if recordCount == 0 || recordCount > MaxNonBlobStageBatchRecords {
			return ErrRecordBoundsExceeded
		}
	case OperationStageAliasesBatch:
		if recordCount == 0 || recordCount > MaxAliasesPerJob {
			return ErrRecordBoundsExceeded
		}
	case OperationStageImagesBatch:
		if recordCount == 0 || recordCount > MaxImagesPerPage {
			return ErrRecordBoundsExceeded
		}
	}
	return nil
}

func evalSHAOperationLimit(operation OperationName) (uint64, error) {
	if _, err := ParseOperationName(string(operation)); err != nil {
		return 0, err
	}
	switch operation {
	case OperationStagePageBlob:
		return MaxPageBlobEvalSHARequestBytes, nil
	case OperationCommit:
		return MaxCommitEvalSHARequestBytes, nil
	case OperationStagePageFields, OperationStageOutlinksBatch, OperationStageDiscoveriesBatch,
		OperationStageAliasesBatch, OperationStageImagesBatch, OperationStageImageManifest:
		return MaxNonBlobStageBatchRequestBytes, nil
	default:
		return MaxOrdinaryEvalSHARequestBytes, nil
	}
}

func respArrayHeaderSize(count uint64) uint64 {
	return 1 + uint64(len(strconv.FormatUint(count, 10))) + 2
}

func addRESPBulkSize(size, length uint64) (uint64, bool) {
	overhead := uint64(5 + len(strconv.FormatUint(length, 10)))
	bulkSize, ok := addSize(length, overhead)
	if !ok {
		return 0, false
	}
	return addSize(size, bulkSize)
}

func addSize(left, right uint64) (uint64, bool) {
	result := left + right
	return result, result >= left
}
