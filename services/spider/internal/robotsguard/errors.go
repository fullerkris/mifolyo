package robotsguard

import (
	"errors"
	"fmt"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawlpolicy"
)

// Reason is a stable, machine-readable robots enforcement failure reason.
type Reason string

const (
	ReasonInvalidArgument     Reason = "invalid_argument"
	ReasonInvalidPageDecision Reason = "invalid_page_decision"
	ReasonRobotsPolicyDenied  Reason = "robots_policy_denied"
	ReasonFetchFailed         Reason = "fetch_failed"
	ReasonUnexpectedStatus    Reason = "unexpected_status"
	ReasonParseFailed         Reason = "parse_failed"
	ReasonWaitCanceled        Reason = "wait_canceled"
	ReasonCacheCapacity       Reason = "cache_capacity"
)

// Error reports a robots enforcement failure. Fallback is the decision used
// for this call; StatusCode is populated only for an unexpected final HTTP
// status. Error deliberately omits URLs and Cause text so logging Error() does
// not expose a page query. Cause remains available to errors.Is/errors.As.
type Error struct {
	Reason     Reason
	Fallback   crawlpolicy.RobotsErrorAction
	StatusCode int
	Cause      error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.StatusCode != 0 {
		return fmt.Sprintf("robotsguard: %s: status=%d fallback=%s", e.Reason, e.StatusCode, e.Fallback)
	}
	return fmt.Sprintf("robotsguard: %s: fallback=%s", e.Reason, e.Fallback)
}

// Unwrap exposes the underlying policy, parser, transport, or context error
// without rendering it in Error().
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Code returns the stable external form of the reason.
func (e *Error) Code() string {
	if e == nil {
		return ""
	}
	return string(e.Reason)
}

// FallbackAllowed reports whether the associated fallback decision is allow.
func (e *Error) FallbackAllowed() bool {
	return e != nil && e.Fallback == crawlpolicy.RobotsErrorAllow
}

// ReasonOf extracts a robots enforcement reason through error wrapping.
func ReasonOf(err error) Reason {
	var robotsError *Error
	if errors.As(err, &robotsError) {
		return robotsError.Reason
	}
	return ""
}

func newError(reason Reason, fallback crawlpolicy.RobotsErrorAction, statusCode int, cause error) *Error {
	return &Error{
		Reason:     reason,
		Fallback:   fallback,
		StatusCode: statusCode,
		Cause:      cause,
	}
}
