package crawljobsv2

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

const (
	// MinOperationalReferenceKeyBytes requires full SHA-256-strength key
	// material. The deployment secret mount remains responsible for generating
	// and isolating that material.
	MinOperationalReferenceKeyBytes = sha256.Size
	operationalReferenceBytes       = 16
)

var (
	ErrInvalidOperationalReferenceKey        = errors.New("crawljobsv2: invalid operational reference key")
	ErrInvalidOperationalReferenceKind       = errors.New("crawljobsv2: invalid operational reference kind")
	ErrInvalidOperationalReferenceIdentifier = errors.New("crawljobsv2: invalid operational reference identifier")
)

type OperationalReferenceKind string

const (
	OperationalReferenceRun    OperationalReferenceKind = "run"
	OperationalReferenceJob    OperationalReferenceKind = "job"
	OperationalReferenceGroup  OperationalReferenceKind = "group"
	OperationalReferenceOrigin OperationalReferenceKind = "origin"
	OperationalReferenceOwner  OperationalReferenceKind = "owner"
	OperationalReferenceScope  OperationalReferenceKind = "scope"
)

// OperationalReference is an approved, deployment-keyed log/metric label. It
// is always 32 lowercase hexadecimal characters when returned by
// DeriveOperationalReference.
type OperationalReference string

// DeriveOperationalReference implements the monitoring contract exactly:
// first16(HMAC-SHA-256(key, F(reference_kind) || F(raw_identifier))). The key is
// used only for this call and is never retained or included in an error.
func DeriveOperationalReference(key []byte, referenceKind OperationalReferenceKind, rawIdentifier string) (OperationalReference, error) {
	if len(key) < MinOperationalReferenceKeyBytes {
		return "", ErrInvalidOperationalReferenceKey
	}
	if err := validateOperationalReferenceIdentifier(referenceKind, rawIdentifier); err != nil {
		return "", err
	}

	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(F([]byte(referenceKind)))
	_, _ = mac.Write(F([]byte(rawIdentifier)))
	sum := mac.Sum(nil)
	return OperationalReference(hex.EncodeToString(sum[:operationalReferenceBytes])), nil
}

func validateOperationalReferenceIdentifier(referenceKind OperationalReferenceKind, rawIdentifier string) error {
	var err error
	switch referenceKind {
	case OperationalReferenceRun:
		_, err = ParseRunID(rawIdentifier)
	case OperationalReferenceJob:
		_, err = ParseJobID(rawIdentifier)
	case OperationalReferenceGroup:
		_, err = ParseGroupID(rawIdentifier)
	case OperationalReferenceOrigin:
		err = validateCanonicalOrigin(CanonicalOrigin(rawIdentifier))
	case OperationalReferenceOwner:
		_, err = ParseOwnerID(rawIdentifier)
	case OperationalReferenceScope:
		if _, rateErr := ParseRateScopeID(rawIdentifier); rateErr == nil {
			return nil
		}
		_, err = parseNonzeroDigest(rawIdentifier)
	default:
		return ErrInvalidOperationalReferenceKind
	}
	if err != nil {
		return ErrInvalidOperationalReferenceIdentifier
	}
	return nil
}
