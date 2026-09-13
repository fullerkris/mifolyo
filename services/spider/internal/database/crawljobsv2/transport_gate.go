package crawljobsv2

import (
	"bytes"
	"errors"
)

var ErrInvalidTransportGate = errors.New("crawljobsv2: invalid transport gate")

// CandidatePhase makes legacy-retirement presence an explicit part of the
// stopped-world candidate sequence. Candidate run preparation and abort happen
// before retirement. The post-retirement phase is reserved for an exact RETIRE
// lost-response reconciliation and PROMOTE.
type CandidatePhase string

const (
	CandidateBeforeLegacyRetirement CandidatePhase = "before_legacy_retirement"
	CandidateAfterLegacyRetirement  CandidatePhase = "after_legacy_retirement"
)

// TransportGateInput contains typed authority artifacts. Nil artifacts encode
// the contract's required zero-byte bulk string, never an unvalidated record.
// CandidatePhase is required only in candidate mode.
type TransportGateInput struct {
	Mode           GateMode
	CandidatePhase CandidatePhase
	BootEpoch      string
	Contract       Digest
	Compatibility  *CompatibilityMarker
	CommitGuard    *StoredCommitGuard
	Legacy         *LegacyRetirementRecord
	AdminFreeze    *AdminFreezeRecord
}

// TransportGate is the exact seven-field prefix accepted by V2 scripts. The
// encoded values are private and defensively copied by Arguments.
type TransportGate struct {
	operation      OperationName
	mode           GateMode
	candidatePhase CandidatePhase
	arguments      [7][]byte
	initialized    bool
}

func NewTransportGate(operation OperationName, input TransportGateInput) (TransportGate, error) {
	if err := ValidateGateMode(operation, input.Mode); err != nil || operation == OperationApproveBoot {
		return TransportGate{}, ErrInvalidTransportGate
	}
	if !isLowerHex(input.BootEpoch, 32) {
		return TransportGate{}, ErrInvalidTransportGate
	}

	gate := TransportGate{
		operation:      operation,
		mode:           input.Mode,
		candidatePhase: input.CandidatePhase,
		initialized:    true,
	}
	gate.arguments[0] = []byte(input.Mode)
	gate.arguments[1] = []byte(input.BootEpoch)

	switch input.Mode {
	case GateBootOnly:
		if input.CandidatePhase != "" || input.Contract != "" || input.Compatibility != nil || input.CommitGuard != nil || input.Legacy != nil || input.AdminFreeze != nil {
			return TransportGate{}, ErrInvalidTransportGate
		}
	case GateCandidate:
		if err := populateCandidateGate(&gate, operation, input); err != nil {
			return TransportGate{}, err
		}
	case GateActive:
		if input.CandidatePhase != "" {
			return TransportGate{}, ErrInvalidTransportGate
		}
		if err := populateActiveGate(&gate, input); err != nil {
			return TransportGate{}, err
		}
	default:
		return TransportGate{}, ErrInvalidTransportGate
	}
	if err := gate.validate(); err != nil {
		return TransportGate{}, err
	}
	return gate, nil
}

func (gate TransportGate) Operation() OperationName { return gate.operation }

func (gate TransportGate) Mode() GateMode { return gate.mode }

func (gate TransportGate) CandidatePhase() CandidatePhase { return gate.candidatePhase }

func (gate TransportGate) Arguments() ([][]byte, error) {
	if err := gate.validate(); err != nil {
		return nil, err
	}
	arguments := make([][]byte, len(gate.arguments))
	for index := range gate.arguments {
		arguments[index] = append([]byte(nil), gate.arguments[index]...)
	}
	return arguments, nil
}

func (gate TransportGate) validate() error {
	if !gate.initialized || gate.operation == OperationApproveBoot || ValidateGateMode(gate.operation, gate.mode) != nil ||
		!bytes.Equal(gate.arguments[0], []byte(gate.mode)) || !isLowerHex(string(gate.arguments[1]), 32) {
		return ErrInvalidTransportGate
	}

	input := TransportGateInput{
		Mode:           gate.mode,
		CandidatePhase: gate.candidatePhase,
		BootEpoch:      string(gate.arguments[1]),
		Contract:       Digest(gate.arguments[2]),
	}
	rebuilt := TransportGate{
		operation:      gate.operation,
		mode:           gate.mode,
		candidatePhase: gate.candidatePhase,
		initialized:    true,
	}
	rebuilt.arguments[0] = []byte(gate.mode)
	rebuilt.arguments[1] = append([]byte(nil), gate.arguments[1]...)

	switch gate.mode {
	case GateBootOnly:
		if gate.candidatePhase != "" {
			return ErrInvalidTransportGate
		}
		for index := 2; index < len(gate.arguments); index++ {
			if len(gate.arguments[index]) != 0 {
				return ErrInvalidTransportGate
			}
		}
		return nil
	case GateCandidate:
		compatibility, err := DecodeCompatibilityMarker(gate.arguments[3])
		if err != nil {
			return ErrInvalidTransportGate
		}
		freeze, err := DecodeAdminFreezeRecord(gate.arguments[6])
		if err != nil {
			return ErrInvalidTransportGate
		}
		input.Compatibility = &compatibility
		input.AdminFreeze = &freeze
		if len(gate.arguments[4]) != 0 {
			return ErrInvalidTransportGate
		}
		if len(gate.arguments[5]) != 0 {
			legacy, err := DecodeLegacyRetirementRecord(gate.arguments[5])
			if err != nil {
				return ErrInvalidTransportGate
			}
			input.Legacy = &legacy
		}
		if err := populateCandidateGate(&rebuilt, gate.operation, input); err != nil {
			return err
		}
	case GateActive:
		if gate.candidatePhase != "" || len(gate.arguments[6]) != 0 {
			return ErrInvalidTransportGate
		}
		compatibility, err := DecodeCompatibilityMarker(gate.arguments[3])
		if err != nil {
			return ErrInvalidTransportGate
		}
		guard, err := DecodeStoredCommitGuard(gate.arguments[4])
		if err != nil {
			return ErrInvalidTransportGate
		}
		legacy, err := DecodeLegacyRetirementRecord(gate.arguments[5])
		if err != nil {
			return ErrInvalidTransportGate
		}
		input.Compatibility = &compatibility
		input.CommitGuard = &guard
		input.Legacy = &legacy
		if err := populateActiveGate(&rebuilt, input); err != nil {
			return err
		}
	default:
		return ErrInvalidTransportGate
	}

	for index := range gate.arguments {
		if !bytes.Equal(gate.arguments[index], rebuilt.arguments[index]) {
			return ErrInvalidTransportGate
		}
	}
	return nil
}

func populateCandidateGate(gate *TransportGate, operation OperationName, input TransportGateInput) error {
	if validateNonzeroDigest(input.Contract) != nil || input.Compatibility == nil || input.CommitGuard != nil || input.AdminFreeze == nil {
		return ErrInvalidTransportGate
	}
	if err := validateCandidatePhase(operation, input.CandidatePhase, input.Legacy != nil); err != nil {
		return err
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

	if input.Legacy == nil {
		return nil
	}
	legacyInput, err := input.Legacy.Input()
	if err != nil || legacyInput.FreezeNonce != freezeInput.FreezeNonce {
		return ErrArtifactMismatch
	}
	gate.arguments[5], err = input.Legacy.Encode()
	return err
}

func populateActiveGate(gate *TransportGate, input TransportGateInput) error {
	if validateNonzeroDigest(input.Contract) != nil || input.Compatibility == nil || input.CommitGuard == nil || input.Legacy == nil || input.AdminFreeze != nil {
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
	if err != nil || core.ContractSHA256() != input.Contract || artifact.CommitGuardDigest() != coreDigest || guardManifestDigest != manifestDigest ||
		artifact.RedisConfigSHA256() != core.RedisConfigSHA256() {
		return ErrArtifactMismatch
	}
	switch core.Cutover() {
	case CutoverFresh:
		if core.CandidateRun() != "" || input.Legacy.V1Count() != 0 {
			return ErrArtifactMismatch
		}
	case CutoverV1Migration:
		if input.Legacy.V1Count() == 0 || validateRunID(core.CandidateRun()) != nil {
			return ErrArtifactMismatch
		}
	default:
		return ErrArtifactMismatch
	}
	gate.arguments[2] = []byte(input.Contract)
	gate.arguments[3] = compatibility
	gate.arguments[4] = guard
	gate.arguments[5] = legacy
	return nil
}

func validateCandidatePhase(operation OperationName, phase CandidatePhase, legacyPresent bool) error {
	switch phase {
	case CandidateBeforeLegacyRetirement:
		if legacyPresent {
			return ErrInvalidTransportGate
		}
		switch operation {
		case OperationRetireLegacyKeys,
			OperationCreateRun, OperationEnqueueBatch, OperationBeginRunAudit, OperationAuditRunBatch,
			OperationSealRun, OperationCancelRun, OperationCancelBatch, OperationPurgeRunBatch:
			return nil
		default:
			return ErrInvalidGateMode
		}
	case CandidateAfterLegacyRetirement:
		if !legacyPresent {
			return ErrInvalidTransportGate
		}
		switch operation {
		case OperationRetireLegacyKeys, OperationPromoteCandidateContracts:
			return nil
		default:
			return ErrInvalidGateMode
		}
	default:
		return ErrInvalidTransportGate
	}
}
