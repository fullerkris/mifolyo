package securefetch

import (
	"errors"
	"fmt"
)

// Reason is a stable, machine-readable secure-fetch failure reason.
type Reason string

const (
	ReasonInvalidConfig         Reason = "invalid_config"
	ReasonInvalidArgument       Reason = "invalid_argument"
	ReasonProxyEnvironment      Reason = "proxy_environment"
	ReasonMatcherDenied         Reason = "matcher_denied"
	ReasonHopDenied             Reason = "hop_denied"
	ReasonInvalidURL            Reason = "invalid_url"
	ReasonAmbiguousPath         Reason = "ambiguous_path"
	ReasonStaticURLDenied       Reason = "static_url_denied"
	ReasonNonDefaultPort        Reason = "non_default_port"
	ReasonInvalidDecision       Reason = "invalid_decision"
	ReasonGateDenied            Reason = "gate_denied"
	ReasonDNSLookup             Reason = "dns_lookup_failed"
	ReasonDNSNoAnswer           Reason = "dns_no_answer"
	ReasonDNSTooManyAnswers     Reason = "dns_too_many_answers"
	ReasonDNSInvalidAnswer      Reason = "dns_invalid_answer"
	ReasonDNSDuplicateAnswer    Reason = "dns_duplicate_answer"
	ReasonDNSProhibitedAddress  Reason = "dns_prohibited_address"
	ReasonDialNetwork           Reason = "dial_network_rejected"
	ReasonDialAuthority         Reason = "dial_authority_rejected"
	ReasonDialFailed            Reason = "dial_failed"
	ReasonRemoteAddressMismatch Reason = "remote_address_mismatch"
	ReasonRequestFailed         Reason = "request_failed"
	ReasonInvalidResponse       Reason = "invalid_response"
	ReasonContentEncoding       Reason = "content_encoding_rejected"
	ReasonBodyTooLarge          Reason = "body_too_large"
	ReasonRedirectLocation      Reason = "redirect_location_invalid"
	ReasonRedirectMode          Reason = "redirect_mode_denied"
	ReasonRedirectHost          Reason = "redirect_host_denied"
	ReasonRedirectGroup         Reason = "redirect_group_denied"
	ReasonHTTPSDowngrade        Reason = "https_downgrade_denied"
	ReasonRedirectCycle         Reason = "redirect_cycle"
	ReasonRedirectHopLimit      Reason = "redirect_hop_limit"
)

// Error is a typed secure-fetch error. Error intentionally omits Cause text so
// a parser, matcher, or transport error cannot copy a sensitive URL query into
// logs. Callers can inspect Cause with errors.Is/errors.As when needed.
type Error struct {
	Reason    Reason
	Operation string
	Cause     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Operation == "" {
		return fmt.Sprintf("securefetch: %s", e.Reason)
	}
	return fmt.Sprintf("securefetch: %s: %s", e.Operation, e.Reason)
}

// Unwrap exposes the underlying error without rendering it in Error().
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

// ReasonOf extracts a secure-fetch reason through error wrapping.
func ReasonOf(err error) Reason {
	var fetchError *Error
	if errors.As(err, &fetchError) {
		return fetchError.Reason
	}
	return ""
}

func newError(reason Reason, operation string, cause error) error {
	return &Error{Reason: reason, Operation: operation, Cause: cause}
}
