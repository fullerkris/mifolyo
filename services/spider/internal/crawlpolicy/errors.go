package crawlpolicy

import (
	"errors"
	"fmt"
)

// DenialReason is a stable machine-readable reason for a Match denial.
type DenialReason string

const (
	ReasonInvalidDepth     DenialReason = "invalid_depth"
	ReasonInvalidURL       DenialReason = "invalid_url"
	ReasonAmbiguousPath    DenialReason = "ambiguous_path"
	ReasonStaticSafety     DenialReason = "static_safety"
	ReasonUnknownDomain    DenialReason = "unknown_domain"
	ReasonGroupDisabled    DenialReason = "group_disabled"
	ReasonSchemeNotAllowed DenialReason = "scheme_not_allowed"
	ReasonPathDenied       DenialReason = "path_denied"
	ReasonPathNotAllowed   DenialReason = "path_not_allowed"
	ReasonDepthExceeded    DenialReason = "depth_exceeded"
)

// DenialError reports a policy denial. Cause preserves a canonicalization or
// static-admission error where one exists; Reason remains stable regardless of
// the wrapped implementation error.
type DenialError struct {
	Reason DenialReason
	Cause  error
}

func (e *DenialError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("crawl denied (%s): %v", e.Reason, e.Cause)
	}
	return fmt.Sprintf("crawl denied (%s)", e.Reason)
}

// Unwrap exposes the underlying canonicalization or static safety error.
func (e *DenialError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Code returns the stable external form of the denial reason.
func (e *DenialError) Code() string {
	if e == nil {
		return ""
	}
	return string(e.Reason)
}

// DenialReasonOf extracts a denial reason through error wrapping.
func DenialReasonOf(err error) DenialReason {
	var denial *DenialError
	if errors.As(err, &denial) {
		return denial.Reason
	}
	return ""
}
