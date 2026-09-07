package crawljobsv2

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalidRunID           = errors.New("crawljobsv2: invalid run ID")
	ErrInvalidJobID           = errors.New("crawljobsv2: invalid job ID")
	ErrInvalidOwnerID         = errors.New("crawljobsv2: invalid owner ID")
	ErrInvalidLeaseToken      = errors.New("crawljobsv2: invalid lease token")
	ErrInvalidFence           = errors.New("crawljobsv2: invalid fence")
	ErrInvalidDigest          = errors.New("crawljobsv2: invalid digest")
	ErrInvalidReservationID   = errors.New("crawljobsv2: invalid reservation ID")
	ErrInvalidRateScopeID     = errors.New("crawljobsv2: invalid rate scope ID")
	ErrInvalidGroupID         = errors.New("crawljobsv2: invalid group ID")
	ErrInvalidRequestKind     = errors.New("crawljobsv2: invalid request kind")
	ErrInvalidUnsignedDecimal = errors.New("crawljobsv2: invalid unsigned decimal")
	ErrInvalidScoreText       = errors.New("crawljobsv2: invalid score text")
	ErrInvalidRedisScore      = errors.New("crawljobsv2: invalid Redis score")
	ErrInvalidCanonicalURL    = errors.New("crawljobsv2: invalid canonical URL")
	ErrURLIdentityMismatch    = errors.New("crawljobsv2: URL identity mismatch")
)

// Identity and opaque protocol values deliberately redact their default
// formatting and generic marshaling (see redaction.go). Protocol codecs must
// validate them and then use an explicit string(value) or []byte(value)
// conversion only at the authenticated wire or digest-framing boundary.
type RunID string
type JobID string
type OwnerID string
type LeaseToken string
type Digest string
type ReservationID string
type RateScopeID string
type GroupID string
type RequestKind string
type UnsignedDecimal string
type ScoreText string
type Fence uint64

const (
	RequestRobots         RequestKind = "robots"
	RequestDocument       RequestKind = "document"
	RequestRedirect       RequestKind = "redirect"
	RequestRenderResource RequestKind = "render_resource"
)

func ParseRunID(value string) (RunID, error) {
	if !isLowerHex(value, 32) {
		return "", ErrInvalidRunID
	}
	return RunID(value), nil
}

func ParseJobID(value string) (JobID, error) {
	if !isLowerHex(value, 64) {
		return "", ErrInvalidJobID
	}
	return JobID(value), nil
}

func ParseOwnerID(value string) (OwnerID, error) {
	if !isLowerHex(value, 32) {
		return "", ErrInvalidOwnerID
	}
	return OwnerID(value), nil
}

func ParseLeaseToken(value string) (LeaseToken, error) {
	if !isLowerHex(value, 64) {
		return "", ErrInvalidLeaseToken
	}
	return LeaseToken(value), nil
}

func ParseDigest(value string) (Digest, error) {
	if !isLowerHex(value, 64) {
		return "", ErrInvalidDigest
	}
	return Digest(value), nil
}

func ParseReservationID(value string) (ReservationID, error) {
	if !isLowerHex(value, 64) {
		return "", ErrInvalidReservationID
	}
	return ReservationID(value), nil
}

func ParseRateScopeID(value string) (RateScopeID, error) {
	if !isLowerHex(value, 32) {
		return "", ErrInvalidRateScopeID
	}
	return RateScopeID(value), nil
}

func ParseGroupID(value string) (GroupID, error) {
	if len(value) == 0 || len(value) > MaxPolicyGroupIDBytes || !utf8.ValidString(value) {
		return "", ErrInvalidGroupID
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", ErrInvalidGroupID
		}
	}
	return GroupID(value), nil
}

func ParseRequestKind(value string) (RequestKind, error) {
	kind := RequestKind(value)
	switch kind {
	case RequestRobots, RequestDocument, RequestRedirect, RequestRenderResource:
		return kind, nil
	default:
		return "", ErrInvalidRequestKind
	}
}

func ParseUnsignedDecimal(value string) (UnsignedDecimal, error) {
	if value == "" || len(value) > len("9007199254740991") {
		return "", ErrInvalidUnsignedDecimal
	}
	if value != "0" && value[0] == '0' {
		return "", ErrInvalidUnsignedDecimal
	}
	for index := range value {
		if value[index] < '0' || value[index] > '9' {
			return "", ErrInvalidUnsignedDecimal
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed > MaxExactInteger {
		return "", ErrInvalidUnsignedDecimal
	}
	return UnsignedDecimal(value), nil
}

func CanonicalUnsignedDecimal(value uint64) (UnsignedDecimal, error) {
	if value > MaxExactInteger {
		return "", ErrInvalidUnsignedDecimal
	}
	return UnsignedDecimal(strconv.FormatUint(value, 10)), nil
}

func (value UnsignedDecimal) Uint64() (uint64, error) {
	if _, err := ParseUnsignedDecimal(string(value)); err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseUint(string(value), 10, 64)
	if err != nil {
		return 0, ErrInvalidUnsignedDecimal
	}
	return parsed, nil
}

func ParseFence(value string) (Fence, error) {
	decimal, err := ParseUnsignedDecimal(value)
	if err != nil {
		return 0, ErrInvalidFence
	}
	parsed, err := decimal.Uint64()
	if err != nil || parsed == 0 {
		return 0, ErrInvalidFence
	}
	return Fence(parsed), nil
}

func NewFence(value uint64) (Fence, error) {
	if value == 0 || value > MaxExactInteger {
		return 0, ErrInvalidFence
	}
	return Fence(value), nil
}

func (fence Fence) Decimal() (UnsignedDecimal, error) {
	if fence == 0 || uint64(fence) > MaxExactInteger {
		return "", ErrInvalidFence
	}
	return UnsignedDecimal(strconv.FormatUint(uint64(fence), 10)), nil
}

var scoreTextPattern = regexp.MustCompile(`^(?:0|-?(?:[1-9][0-9]*(?:\.[0-9]{0,5}[1-9])?|0\.[0-9]{0,5}[1-9]))\z`)

func ParseScoreText(value string) (ScoreText, error) {
	if !scoreTextPattern.MatchString(value) {
		return "", ErrInvalidScoreText
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < -1000 || parsed > 10000 {
		return "", ErrInvalidScoreText
	}
	return ScoreText(value), nil
}

func (score ScoreText) Float64() (float64, error) {
	if err := validateScoreText(score); err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseFloat(string(score), 64)
	if err != nil {
		return 0, ErrInvalidScoreText
	}
	return parsed, nil
}

// ValidateRedisScore requires a stored ZSET score to be finite and to have the
// same binary64 value as its separately stored canonical score text.
func ValidateRedisScore(score ScoreText, redisValue string) error {
	canonical, err := score.Float64()
	if err != nil {
		return err
	}
	parsed, err := strconv.ParseFloat(redisValue, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || math.Float64bits(parsed) != math.Float64bits(canonical) {
		return ErrInvalidRedisScore
	}
	return nil
}

func validateRunID(value RunID) error {
	_, err := ParseRunID(string(value))
	return err
}

func validateJobID(value JobID) error {
	_, err := ParseJobID(string(value))
	return err
}

func validateOwnerID(value OwnerID) error {
	_, err := ParseOwnerID(string(value))
	return err
}

func validateLeaseToken(value LeaseToken) error {
	_, err := ParseLeaseToken(string(value))
	return err
}

func validateDigest(value Digest) error {
	_, err := ParseDigest(string(value))
	return err
}

func validateReservationID(value ReservationID) error {
	_, err := ParseReservationID(string(value))
	return err
}

func validateRateScopeID(value RateScopeID) error {
	_, err := ParseRateScopeID(string(value))
	return err
}

func validateGroupID(value GroupID) error {
	_, err := ParseGroupID(string(value))
	return err
}

func validateRequestKind(value RequestKind) error {
	_, err := ParseRequestKind(string(value))
	return err
}

func validateScoreText(value ScoreText) error {
	_, err := ParseScoreText(string(value))
	return err
}

func validateNonnegativeExactInteger(value uint64) error {
	if value > MaxExactInteger {
		return ErrInvalidUnsignedDecimal
	}
	return nil
}

func canonicalDecimal(value uint64) string {
	return strconv.FormatUint(value, 10)
}

func isLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for index := range value {
		if (value[index] < '0' || value[index] > '9') && (value[index] < 'a' || value[index] > 'f') {
			return false
		}
	}
	return true
}
