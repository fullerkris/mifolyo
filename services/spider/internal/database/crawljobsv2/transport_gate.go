package crawljobsv2

import "errors"

var ErrInvalidTransportGate = errors.New("crawljobsv2: invalid transport gate")

// TransportGateInput contains typed authority artifacts. Nil artifacts encode
// the contract's required zero-byte bulk string, never an unvalidated record.
type TransportGateInput struct {
	Mode          GateMode
	BootEpoch     string
	Contract      Digest
	Compatibility *CompatibilityMarker
	CommitGuard   *StoredCommitGuard
	Legacy        *LegacyRetirementRecord
	AdminFreeze   *AdminFreezeRecord
	SourceKind    SourceKind
}

// TransportGate is the exact seven-field prefix accepted by V2 scripts. The
// encoded values are private and defensively copied by Arguments.
type TransportGate struct {
	operation   OperationName
	mode        GateMode
	arguments   [7][]byte
	initialized bool
}

func NewTransportGate(operation OperationName, input TransportGateInput) (TransportGate, error) {
	if err := ValidateGateMode(operation, input.Mode); err != nil || operation == OperationApproveBoot {
		return TransportGate{}, ErrInvalidTransportGate
	}
	if !isLowerHex(input.BootEpoch, 32) {
		return TransportGate{}, ErrInvalidTransportGate
	}
	if err := validateCandidateSemanticPath(operation, input.Mode, input.SourceKind); err != nil {
		return TransportGate{}, err
	}

	gate := TransportGate{operation: operation, mode: input.Mode, initialized: true}
	gate.arguments[0] = []byte(input.Mode)
	gate.arguments[1] = []byte(input.BootEpoch)

	switch input.Mode {
	case GateBootOnly:
		if input.Contract != "" || input.Compatibility != nil || input.CommitGuard != nil || input.Legacy != nil || input.AdminFreeze != nil {
			return TransportGate{}, ErrInvalidTransportGate
		}
	case GateCandidate:
		if err := populateCandidateGate(&gate, operation, input); err != nil {
			return TransportGate{}, err
		}
	case GateActive:
		if err := populateActiveGate(&gate, input); err != nil {
			return TransportGate{}, err
		}
	default:
		return TransportGate{}, ErrInvalidTransportGate
	}
	return gate, nil
}

func (gate TransportGate) Operation() OperationName { return gate.operation }

func (gate TransportGate) Mode() GateMode { return gate.mode }

func (gate TransportGate) Arguments() ([][]byte, error) {
	if !gate.initialized {
		return nil, ErrInvalidTransportGate
	}
	arguments := make([][]byte, len(gate.arguments))
	for index := range gate.arguments {
		arguments[index] = append([]byte(nil), gate.arguments[index]...)
	}
	return arguments, nil
}

func populateCandidateGate(gate *TransportGate, operation OperationName, input TransportGateInput) error {
	if validateDigest(input.Contract) != nil || input.Compatibility == nil || input.CommitGuard != nil || input.AdminFreeze == nil {
		return ErrInvalidTransportGate
	}
	compatibility, err := input.Compatibility.Encode()
	if err != nil {
		return err
	}
	freeze, err := input.AdminFreeze.Encode()
	if err != nil {
		return err
	}
	manifestDigest, err := input.Compatibility.ManifestSHA256()
	if err != nil {
		return err
	}
	freezeInput, err := input.AdminFreeze.Input()
	if err != nil || freezeInput.CandidateManifestSHA256 != manifestDigest || freezeInput.CandidateContractSHA256 != input.Contract {
		return ErrArtifactMismatch
	}
	gate.arguments[2] = []byte(input.Contract)
	gate.arguments[3] = compatibility
	gate.arguments[6] = freeze

	if operation == OperationRetireLegacyKeys {
		if input.Legacy != nil {
			return ErrInvalidTransportGate
		}
		return nil
	}
	if input.Legacy == nil {
		return ErrInvalidTransportGate
	}
	legacyInput, err := input.Legacy.Input()
	if err != nil || legacyInput.FreezeNonce != freezeInput.FreezeNonce {
		return ErrArtifactMismatch
	}
	gate.arguments[5], err = input.Legacy.Encode()
	return err
}

func populateActiveGate(gate *TransportGate, input TransportGateInput) error {
	if validateDigest(input.Contract) != nil || input.Compatibility == nil || input.CommitGuard == nil || input.Legacy == nil || input.AdminFreeze != nil {
		return ErrInvalidTransportGate
	}
	compatibility, err := input.Compatibility.Encode()
	if err != nil {
		return err
	}
	guard, err := input.CommitGuard.Encode()
	if err != nil {
		return err
	}
	legacy, err := input.Legacy.Encode()
	if err != nil {
		return err
	}
	artifact, err := input.Compatibility.Artifact()
	if err != nil {
		return err
	}
	manifestDigest, err := input.Compatibility.ManifestSHA256()
	if err != nil {
		return err
	}
	guardManifestDigest, err := input.CommitGuard.CompatibilityManifestSHA256()
	if err != nil {
		return err
	}
	core, err := input.CommitGuard.GuardCore()
	if err != nil {
		return err
	}
	coreDigest, err := core.SHA256()
	if err != nil || core.ContractSHA256() != input.Contract || artifact.CommitGuardDigest() != coreDigest || guardManifestDigest != manifestDigest {
		return ErrArtifactMismatch
	}
	gate.arguments[2] = []byte(input.Contract)
	gate.arguments[3] = compatibility
	gate.arguments[4] = guard
	gate.arguments[5] = legacy
	return nil
}

func validateCandidateSemanticPath(operation OperationName, mode GateMode, sourceKind SourceKind) error {
	if mode != GateCandidate {
		if operation == OperationCreateRun && sourceKind != SourceMongo {
			return ErrInvalidSourceKind
		}
		return nil
	}
	switch operation {
	case OperationRetireLegacyKeys, OperationPromoteCandidateContracts:
		return nil
	case OperationCreateRun, OperationEnqueueBatch, OperationBeginRunAudit, OperationAuditRunBatch,
		OperationSealRun, OperationCancelRun, OperationCancelBatch, OperationPurgeRunBatch:
		if sourceKind != SourceV1Migration {
			return ErrInvalidSourceKind
		}
		return nil
	default:
		return ErrInvalidGateMode
	}
}
